package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/snirye/youtube-guardian/backend/internal/store"
)

type Bot struct {
	data     store.Store
	parentID int64
	baseURL  string
	token    string
	http     *http.Client
}

func New(data store.Store, token string, parentID int64, transport http.RoundTripper) *Bot {
	return &Bot{
		data: data, parentID: parentID, token: token, baseURL: "https://api.telegram.org",
		http: &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
	}
}

type user struct{ ID int64 }
type chat struct {
	ID   int64
	Type string
}
type message struct {
	MessageID int64 `json:"message_id"`
	From      *user
	Chat      chat
	Text      string
}
type callback struct {
	ID      string
	From    user
	Message *message
	Data    string
}
type update struct {
	ID       int64 `json:"update_id"`
	Message  *message
	Callback *callback `json:"callback_query"`
}
type button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}
type keyboard struct {
	Rows [][]button `json:"inline_keyboard"`
}
type command struct {
	Name        string `json:"command"`
	Description string `json:"description"`
}

var commands = []command{
	{"start", "Overview"}, {"status", "Backend and Chrome status"}, {"pending", "Pending channel requests"},
	{"allow", "Approve a channel by ID"}, {"block", "Block a channel by ID"}, {"channels", "List approved channels"},
	{"logs", "View recent activity"}, {"shorts_on", "Hide and block Shorts"}, {"shorts_off", "Use channel rules for Shorts"},
	{"help", "List commands"},
}

func (b *Bot) call(ctx context.Context, method string, payload any, output any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.baseURL+"/bot"+b.token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build Telegram %s request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := b.http.Do(req)
	if err != nil {
		// Transport errors can include the bot token in the request URL.
		return fmt.Errorf("Telegram %s connection failed", method)
	}
	defer response.Body.Close()
	var result struct {
		OK        bool
		Result    json.RawMessage
		ErrorCode int `json:"error_code"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result); err != nil {
		return fmt.Errorf("Telegram %s returned an invalid response (HTTP %d)", method, response.StatusCode)
	}
	if response.StatusCode != http.StatusOK || !result.OK {
		return fmt.Errorf("Telegram %s failed (HTTP %d, API %d)", method, response.StatusCode, result.ErrorCode)
	}
	if output != nil {
		if err := json.Unmarshal(result.Result, output); err != nil {
			return fmt.Errorf("Telegram %s returned invalid data", method)
		}
	}
	return nil
}

func (b *Bot) Run(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for ctx.Err() == nil {
			if err := b.notify(ctx); err != nil && ctx.Err() == nil {
				log.Printf("notify pending Telegram requests: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { <-done }()

	var registered bool
	for ctx.Err() == nil {
		if !registered {
			err := b.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil)
			registered = err == nil
			if err != nil && ctx.Err() == nil {
				log.Printf("register Telegram commands: %v", err)
			}
		}
		err := b.poll(ctx)
		if err == nil || ctx.Err() != nil {
			continue
		}
		log.Printf("poll Telegram: %v", err)
		timer := time.NewTimer(5 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (b *Bot) poll(ctx context.Context) error {
	offset, err := b.data.TelegramOffset(ctx)
	if err != nil {
		return err
	}
	var updates []update
	if err := b.call(ctx, "getUpdates", map[string]any{
		"offset": offset, "timeout": 20, "allowed_updates": []string{"message", "callback_query"},
	}, &updates); err != nil {
		return err
	}
	for _, event := range updates {
		if event.ID < offset {
			continue
		}
		if err := b.handle(ctx, event); err != nil {
			log.Printf("handle Telegram update: %v", err)
		}
		offset = event.ID + 1
		if err := b.data.SetTelegramOffset(ctx, offset); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) notify(ctx context.Context) error {
	pending, err := b.data.Pending(ctx, true, 20)
	if err != nil {
		return err
	}
	for _, r := range pending {
		keys := keyboard{Rows: [][]button{{
			{Text: "Approve", Data: fmt.Sprintf("allow:%d", r.ID)},
			{Text: "Reject", Data: fmt.Sprintf("reject:%d", r.ID)},
		}}}
		if err := b.send(ctx, formatRequest(r), &keys); err != nil {
			return err
		}
		if err := b.data.MarkNotified(ctx, r.ID); err != nil {
			return err
		}
	}
	return nil
}

func formatRequest(r store.Request) string {
	return fmt.Sprintf("%s (%s)\n%s\nRequested %s", r.ChannelName, r.ChannelID, r.VideoURL,
		time.UnixMilli(r.RequestedAt).UTC().Format(time.RFC3339))
}

func (b *Bot) send(ctx context.Context, text string, keys *keyboard) error {
	const maxRunes = 3800
	if runes := []rune(text); len(runes) > maxRunes {
		text = string(runes[:maxRunes]) + "…"
	}
	return b.call(ctx, "sendMessage", struct {
		ChatID         int64     `json:"chat_id"`
		Text           string    `json:"text"`
		DisablePreview bool      `json:"disable_web_page_preview"`
		Keys           *keyboard `json:"reply_markup,omitempty"`
	}{b.parentID, text, true, keys}, nil)
}

func (b *Bot) privateParent(c chat) bool {
	return c.Type == "private" && c.ID == b.parentID
}

func (b *Bot) handle(ctx context.Context, u update) error {
	if query := u.Callback; query != nil {
		if query.From.ID != b.parentID || query.Message == nil || !b.privateParent(query.Message.Chat) {
			return nil
		}
		return b.handleCallback(ctx, query)
	}
	m := u.Message
	if m == nil || m.From == nil || m.From.ID != b.parentID || !b.privateParent(m.Chat) {
		return nil
	}
	fields := strings.Fields(m.Text)
	if len(fields) == 0 || !strings.HasPrefix(fields[0], "/") {
		return nil
	}
	name, _, _ := strings.Cut(fields[0], "@")
	text, err := b.execute(ctx, name, fields[1:])
	if err != nil {
		log.Printf("Telegram command failed: %v", err)
		text = "The local operation failed. Please try again."
	}
	return b.send(ctx, text, nil)
}

func (b *Bot) handleCallback(ctx context.Context, q *callback) error {
	action, rawID, ok := strings.Cut(q.Data, ":")
	id, err := strconv.ParseInt(rawID, 10, 64)
	text := "Invalid approval button."
	resolved := false
	if ok && err == nil && id > 0 && (action == "allow" || action == "reject") {
		err = b.data.Resolve(ctx, id, action == "allow")
		switch {
		case err == nil:
			resolved = true
			text = "Channel blocked."
			if action == "allow" {
				text = "Channel approved."
			}
		case errors.Is(err, store.ErrNotPending):
			resolved = true
			text = "This request is no longer pending."
		default:
			log.Printf("resolve approval request: %v", err)
			text = "Could not resolve the request. Try again."
		}
	}
	if err := b.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": q.ID, "text": text}, nil); err != nil {
		return err
	}
	if resolved {
		return b.call(ctx, "editMessageReplyMarkup", map[string]any{
			"chat_id": b.parentID, "message_id": q.Message.MessageID,
			"reply_markup": keyboard{Rows: [][]button{}},
		}, nil)
	}
	return nil
}
