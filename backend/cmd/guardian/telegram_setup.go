package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/skip2/go-qrcode"
	"github.com/snirye/youtube-guardian/backend/internal/telegram"
)

func setupTelegram(mode string) error {
	if mode == "managed" {
		info, err := os.Stdout.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return errors.New("run managed Telegram setup through the PowerShell installer")
		}
	}

	input := bufio.NewScanner(io.LimitReader(os.Stdin, 513))
	if !input.Scan() {
		return errors.New("Telegram setup requires a bot token on standard input")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, telegram.SetupTimeout)
	defer cancel()

	result, err := telegram.Setup(ctx, mode, strings.TrimSpace(input.Text()), func(link string) {
		printTelegramLink(os.Stderr, mode, link)
	}, nil)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(result)
}

func printTelegramLink(out io.Writer, mode, link string) {
	fmt.Fprintln(out, "Scan this QR code with the parent's phone, or open the Telegram link.")
	if mode == "managed" {
		fmt.Fprintln(out, "Tap Create Bot and keep the suggested username.")
	} else {
		fmt.Fprintln(out, "Tap Start in the private chat to pair this account.")
	}

	qr, err := qrcode.New(link, qrcode.Medium)
	if err == nil {
		bitmap := qr.Bitmap()
		const reset = "\x1b[0m"
		for y := 0; y < len(bitmap); y += 2 {
			var line strings.Builder
			for x, darkTop := range bitmap[y] {
				darkBottom := y+1 < len(bitmap) && bitmap[y+1][x]
				switch {
				case darkTop && darkBottom:
					line.WriteString("\x1b[40m ")
				case darkTop:
					line.WriteString("\x1b[30;47m▀")
				case darkBottom:
					line.WriteString("\x1b[37;40m▀")
				default:
					line.WriteString("\x1b[47m ")
				}
			}
			fmt.Fprintln(out, line.String()+reset)
		}
	}
	fmt.Fprintln(out, link)
	fmt.Fprintln(out, "Waiting for Telegram (up to 3 minutes)...")
}
