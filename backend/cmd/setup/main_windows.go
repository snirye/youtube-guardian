//go:build guardian_setup

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lxn/walk"
	dec "github.com/lxn/walk/declarative"
	"github.com/skip2/go-qrcode"
	"github.com/snirye/youtube-guardian/backend/internal/config"
	"github.com/snirye/youtube-guardian/backend/internal/telegram"
	"golang.org/x/sys/windows"
)

const title = "YouTube Guardian Setup"

type wizard struct {
	window   *walk.MainWindow
	heading  *walk.Label
	details  *walk.TextEdit
	status   *walk.Label
	token    *walk.LineEdit
	qr       *walk.ImageView
	qrBitmap *walk.Bitmap
	copy     *walk.PushButton
	next     *walk.PushButton
	action   func()
	link     string
	busy     bool
	ctx      context.Context
}

func main() {
	if err := runWizard(); err != nil {
		walk.MsgBox(nil, title, err.Error(), walk.MsgBoxIconError)
		os.Exit(1)
	}
}

func runWizard() error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	name, err := windows.UTF16PtrFromString(`Global\YouTubeGuardianSetup`)
	if err != nil {
		return err
	}
	lock, err := windows.CreateMutex(nil, false, name)
	if lock != 0 {
		defer windows.CloseHandle(lock)
	}
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return errors.New("YouTube Guardian Setup is already open. Close it or return to that window.")
	}
	if err != nil {
		return err
	}
	paths, err := systemPaths()
	if err != nil {
		return err
	}
	reuse, err := paths.hasConfiguration()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := wizard{ctx: ctx}
	if err := (dec.MainWindow{
		AssignTo: &w.window,
		Title:    title,
		Size:     dec.Size{Width: 580, Height: 520},
		MinSize:  dec.Size{Width: 550, Height: 475},
		Layout:   dec.VBox{Margins: dec.Margins{Left: 24, Right: 24, Top: 20, Bottom: 20}, Spacing: 12},
		Children: []dec.Widget{
			dec.Label{AssignTo: &w.heading, Font: dec.Font{PointSize: 16, Bold: true}},
			dec.TextEdit{AssignTo: &w.details, ReadOnly: true, MinSize: dec.Size{Height: 110}, StretchFactor: 1},
			dec.LineEdit{AssignTo: &w.token, PasswordMode: true, CueBanner: "Paste the bot token from BotFather"},
			dec.ImageView{AssignTo: &w.qr, MinSize: dec.Size{Width: 176, Height: 176},
				MaxSize: dec.Size{Width: 176, Height: 176}, Mode: dec.ImageViewModeIdeal},
			dec.Label{AssignTo: &w.status},
			dec.Composite{Layout: dec.HBox{MarginsZero: true}, Children: []dec.Widget{
				dec.PushButton{AssignTo: &w.copy, OnClicked: func() {
					if err := walk.Clipboard().SetText(w.link); err != nil {
						w.report(err)
					}
				}},
				dec.HSpacer{},
				dec.PushButton{Text: "Close", OnClicked: func() { w.window.Close() }},
				dec.PushButton{AssignTo: &w.next, OnClicked: func() {
					if w.action != nil {
						w.action()
					}
				}},
			}},
		},
	}).Create(); err != nil {
		return err
	}
	w.window.Closing().Attach(func(canceled *bool, _ walk.CloseReason) {
		*canceled = w.busy
		if !w.busy {
			w.clearQR()
			cancel()
		}
	})
	if reuse {
		w.page("Repair or upgrade", "Setup found an existing installation. It will replace the program files and keep the Telegram account, approved channels, settings, and activity.", "Repair or upgrade", func() {
			w.install(paths, "", 0)
		})
	} else {
		w.telegramPage(paths)
	}
	w.window.Run()
	return nil
}

func (w *wizard) page(heading, instructions, button string, action func()) {
	w.clearQR()
	w.heading.SetText(heading)
	w.details.SetText(strings.ReplaceAll(instructions, "\n", "\r\n"))
	w.status.SetText("")
	w.token.SetVisible(false)
	w.copy.SetVisible(false)
	w.next.SetText(button)
	w.next.SetEnabled(action != nil)
	w.action = action
}

func (w *wizard) setCopy(label, value string) {
	w.link = value
	w.copy.SetText(label)
	w.copy.SetVisible(true)
}

func (w *wizard) clearQR() {
	w.qr.SetVisible(false)
	w.qr.SetImage(nil)
	if w.qrBitmap != nil {
		w.qrBitmap.Dispose()
		w.qrBitmap = nil
	}
}

func (w *wizard) report(err error) {
	walk.MsgBox(w.window, title, err.Error(), walk.MsgBoxIconError)
}

func (w *wizard) telegramPage(paths installPaths) {
	w.page("1. Connect Telegram", "On the parent's phone, open @BotFather in Telegram and send /newbot. Paste the token BotFather provides, then select Pair Telegram.", "Pair Telegram", func() {
		token := strings.TrimSpace(w.token.Text())
		if token == "" {
			w.report(errors.New("Paste your BotFather token first."))
			return
		}
		w.token.SetText("")
		w.pair(paths, token)
	})
	w.setCopy("Copy BotFather link", "https://t.me/BotFather")
	w.token.SetVisible(true)
}

func (w *wizard) pair(paths installPaths, token string) {
	w.page("1. Connect Telegram", "Contacting Telegram…", "Waiting for Telegram", nil)
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, telegram.SetupTimeout)
		defer cancel()
		result, err := telegram.Setup(ctx, "pair", token, func(link string) {
			w.sync(func() {
				w.details.SetText("Scan the QR code with the parent's phone. In the private Telegram chat, tap Start. The code expires after three minutes.")
				w.setCopy("Copy pairing link", link)
				qr, err := qrcode.New(link, qrcode.Medium)
				if err == nil {
					w.qrBitmap, err = walk.NewBitmapFromImage(qr.Image(168))
				}
				if err == nil {
					w.qr.SetImage(w.qrBitmap)
					w.qr.SetVisible(true)
				}
			})
		}, nil)
		w.sync(func() {
			if err != nil {
				w.telegramPage(paths)
				w.report(err)
				return
			}
			account := fmt.Sprintf("Telegram user ID %d", result.ParentID)
			if result.ParentUsername != "" {
				account = fmt.Sprintf("@%s (ID %d)", result.ParentUsername, result.ParentID)
			}
			w.page("2. Confirm the parent account", fmt.Sprintf("%s will approve requests through @%s. Check the account and select Install.", account, result.BotUsername), "Install", func() {
				w.install(paths, token, result.ParentID)
			})
			w.setCopy("Copy bot link", "https://t.me/"+result.BotUsername)
		})
	}()
}

func (w *wizard) install(paths installPaths, token string, parent int64) {
	w.busy = true
	w.page("Installing YouTube Guardian", "Setup is installing and checking the Windows service. Keep this window open.", "Installing…", nil)
	go func() {
		cfg, err := install(paths, token, parent)
		installedAt := time.Now()
		w.sync(func() {
			w.busy = false
			if err != nil {
				w.page("Installation needs attention", "Setup could not finish. Review the error and select Retry to continue.", "Retry", func() {
					w.install(paths, token, parent)
				})
				w.report(err)
				return
			}
			w.chrome(paths, cfg, installedAt)
		})
	}()
}

func (w *wizard) chrome(paths installPaths, cfg config.Config, installedAt time.Time) {
	w.page("Load the extension in Chrome", "The Windows service is running. Keep this window open and switch to the child's Windows account if needed.\n\n"+
		"1. Open chrome://extensions in the child's Chrome profile.\n"+
		"2. Turn on Developer mode, choose Load unpacked, and select the folder shown below. If already installed, use the extension's Reload button.\n"+
		"3. Open chrome://policy and click Reload policies. Return here and select Check Chrome.\n\n"+
		filepath.Join(paths.program, "extension"), "Check Chrome", func() {
		w.checkChrome(paths, cfg, installedAt)
	})
	w.setCopy("Copy extension folder", filepath.Join(paths.program, "extension"))
}

func (w *wizard) checkChrome(paths installPaths, cfg config.Config, installedAt time.Time) {
	w.next.SetEnabled(false)
	w.status.SetText("Waiting for Chrome to contact the service (up to 45 seconds)…")
	go func() {
		ctx, cancel := context.WithTimeout(w.ctx, 45*time.Second)
		defer cancel()
		err := retry(ctx, func() error {
			var status struct{ ConnectedAt *time.Time }
			if err := localRequest(cfg, "/heartbeat", &status); err != nil {
				return err
			}
			if status.ConnectedAt == nil || status.ConnectedAt.Before(installedAt) ||
				time.Since(*status.ConnectedAt) > time.Minute {
				return errors.New("no recent Chrome connection")
			}
			return nil
		})
		w.sync(func() {
			if err != nil {
				w.chrome(paths, cfg, installedAt)
				w.status.SetText("Chrome has not checked in. Reload the extension and policies, then try again.")
				return
			}
			w.page("Chrome is connected", "The extension contacted the Windows service and reported a channel configuration. Open YouTube in the child's profile to try a video. You can send /status to your Telegram bot to check the connection later.", "Finish", func() {
				w.window.Close()
			})
		})
	}()
}

func (w *wizard) sync(f func()) {
	if w.ctx.Err() == nil {
		w.window.Synchronize(func() {
			if w.ctx.Err() == nil {
				f()
			}
		})
	}
}
