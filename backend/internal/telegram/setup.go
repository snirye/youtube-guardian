package telegram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const SetupTimeout = 3 * time.Minute

var (
	tokenPattern    = regexp.MustCompile(`^[0-9]+:[A-Za-z0-9_-]{15,}$`)
	usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]{5,32}$`)
)

type SetupResult struct {
	ParentID       int64  `json:"parent_id"`
	ParentUsername string `json:"parent_username,omitempty"`
	BotUsername    string `json:"bot_username"`
	BotToken       string `json:"bot_token,omitempty"`
}

// Setup pairs a private chat with an existing bot or creates a managed bot.
// Credentials go to Telegram directly. showLink is called before polling starts.
func Setup(ctx context.Context, mode, token string, showLink func(string), transport http.RoundTripper) (SetupResult, error) {
	if mode != "pair" && mode != "managed" {
		return SetupResult{}, errors.New("Telegram setup mode must be pair or managed")
	}
	if !tokenPattern.MatchString(token) {
		return SetupResult{}, errors.New("invalid Telegram bot token format")
	}

	api := newClient(token, transport)
	bot, err := botIdentity(ctx, api)
	if err != nil {
		return SetupResult{}, err
	}
	if mode == "managed" {
		return createManagedBot(ctx, api, bot, showLink)
	}

	code := "guardian_" + rand.Text()
	showLink("https://t.me/" + bot.Username + "?start=" + code)
	event, err := waitForUpdate(ctx, api, "message", func(u update) bool {
		m := u.Message
		return m != nil && m.From != nil && m.From.ID > 0 && !m.From.IsBot &&
			m.Chat.Type == "private" && m.Chat.ID == m.From.ID && m.Text == "/start "+code
	})
	if err != nil {
		return SetupResult{}, err
	}
	return SetupResult{
		ParentID: event.Message.From.ID, ParentUsername: event.Message.From.Username,
		BotUsername: bot.Username,
	}, nil
}

func createManagedBot(ctx context.Context, api client, manager user, showLink func(string)) (SetupResult, error) {
	if !manager.CanManageBots {
		return SetupResult{}, errors.New("enable Bot Management Mode for the manager bot in BotFather")
	}

	var random [10]byte
	rand.Read(random[:])
	username := "guard_" + hex.EncodeToString(random[:]) + "_bot"
	link := url.URL{Scheme: "https", Host: "t.me", Path: "/newbot/" + manager.Username + "/" + username}
	link.RawQuery = url.Values{"name": {"YouTube Guardian"}}.Encode()
	showLink(link.String())

	event, err := waitForUpdate(ctx, api, "managed_bot", func(u update) bool {
		m := u.ManagedBot
		return m != nil && m.User.ID > 0 && !m.User.IsBot && m.Bot.ID > 0 && m.Bot.IsBot &&
			strings.EqualFold(m.Bot.Username, username)
	})
	if err != nil {
		return SetupResult{}, err
	}
	created := event.ManagedBot
	var token string
	if err := api.call(ctx, "getManagedBotToken", map[string]int64{"user_id": created.Bot.ID}, &token); err != nil {
		return SetupResult{}, err
	}
	if !tokenPattern.MatchString(token) {
		return SetupResult{}, errors.New("Telegram returned an invalid managed bot token")
	}
	managedAPI := api
	managedAPI.token = token
	bot, err := botIdentity(ctx, managedAPI)
	if err != nil {
		return SetupResult{}, err
	}
	if bot.ID != created.Bot.ID || !strings.EqualFold(bot.Username, username) {
		return SetupResult{}, errors.New("Telegram returned credentials for a different bot")
	}
	return SetupResult{
		ParentID: created.User.ID, ParentUsername: created.User.Username,
		BotUsername: bot.Username, BotToken: token,
	}, nil
}

func botIdentity(ctx context.Context, api client) (user, error) {
	var bot user
	if err := api.call(ctx, "getMe", struct{}{}, &bot); err != nil {
		return user{}, err
	}
	if bot.ID <= 0 || !bot.IsBot || !usernamePattern.MatchString(bot.Username) {
		return user{}, errors.New("Telegram did not return a valid bot identity")
	}
	return bot, nil
}

func waitForUpdate(ctx context.Context, api client, kind string, matches func(update) bool) (update, error) {
	var offset int64
	for {
		var updates []update
		err := api.call(ctx, "getUpdates", map[string]any{
			"offset": offset, "timeout": 15, "allowed_updates": []string{kind},
		}, &updates)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return update{}, fmt.Errorf("Telegram setup timed out after %s. Run setup for a fresh QR code", SetupTimeout)
		}
		if ctx.Err() != nil {
			return update{}, ctx.Err()
		}
		if err != nil {
			return update{}, err
		}
		for _, item := range updates {
			if item.ID < offset {
				continue
			}
			if matches(item) {
				return item, nil
			}
			offset = item.ID + 1
		}
	}
}
