package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

const (
	setupToken   = "12345:manager-or-existing-example"
	managedToken = "67890:managed-bot-example-token"
)

func TestPairExistingBotRequiresPrivateStartCode(t *testing.T) {
	var code string
	polls := 0
	transport := setupTransport(func(req *http.Request) any {
		switch req.URL.Path {
		case "/bot" + setupToken + "/getMe":
			return user{ID: 12345, IsBot: true, Username: "guardian_test_bot"}
		case "/bot" + setupToken + "/getUpdates":
			var in struct {
				Offset int64
			}
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			polls++
			if polls == 1 {
				return []update{
					startUpdate(1, 111, 111, "private", "previous_pairing"),
					startUpdate(2, 222, -222, "group", code),
					startUpdate(3, 333, 334, "private", code),
				}
			}
			if in.Offset != 4 {
				t.Fatalf("poll offset = %d, want 4", in.Offset)
			}
			return []update{startUpdate(4, 444, 444, "private", code)}
		default:
			t.Fatalf("unexpected Telegram method")
			return nil
		}
	})
	result, err := Setup(context.Background(), "pair", setupToken, func(link string) {
		u, err := url.Parse(link)
		if err != nil || u.Host != "t.me" || u.Path != "/guardian_test_bot" {
			t.Fatalf("invalid bot link: %q", link)
		}
		code = u.Query().Get("start")
		if !strings.HasPrefix(code, "guardian_") || len(code) > 64 || strings.Contains(link, setupToken) {
			t.Fatalf("invalid start link: %q", link)
		}
	}, transport)
	if err != nil || result.ParentID != 444 || result.BotUsername != "guardian_test_bot" || result.BotToken != "" {
		t.Fatalf("pair did not return the correct account: %v", err)
	}
}

func TestManagedSetupRetrievesOnlyTheBotCreatedFromTheLink(t *testing.T) {
	var username string
	transport := setupTransport(func(req *http.Request) any {
		switch req.URL.Path {
		case "/bot" + setupToken + "/getMe":
			return user{ID: 12345, IsBot: true, Username: "guardian_manager_bot", CanManageBots: true}
		case "/bot" + setupToken + "/getUpdates":
			return []update{
				{ID: 1, ManagedBot: &managedBot{User: user{ID: 111}, Bot: user{ID: 222, IsBot: true, Username: "different_bot"}}},
				{ID: 2, ManagedBot: &managedBot{User: user{ID: 444, Username: "parent_account"}, Bot: user{ID: 67890, IsBot: true, Username: username}}},
			}
		case "/bot" + setupToken + "/getManagedBotToken":
			var in map[string]int64
			if err := json.NewDecoder(req.Body).Decode(&in); err != nil || in["user_id"] != 67890 {
				t.Fatal("requested credentials for a bot unrelated to the link", err)
			}
			return managedToken
		case "/bot" + managedToken + "/getMe":
			return user{ID: 67890, IsBot: true, Username: username}
		default:
			t.Fatalf("unexpected Telegram method")
			return nil
		}
	})
	result, err := Setup(context.Background(), "managed", setupToken, func(link string) {
		u, err := url.Parse(link)
		prefix := "/newbot/guardian_manager_bot/"
		if err != nil || u.Host != "t.me" || !strings.HasPrefix(u.Path, prefix) || u.Query().Get("name") != "YouTube Guardian" {
			t.Fatalf("invalid managed bot link: %q", link)
		}
		username = strings.TrimPrefix(u.Path, prefix)
		if !usernamePattern.MatchString(username) || !strings.HasSuffix(username, "_bot") || strings.Contains(link, setupToken) {
			t.Fatal("invalid suggested bot username")
		}
	}, transport)
	if err != nil || result.ParentID != 444 || result.ParentUsername != "parent_account" ||
		result.BotUsername != username || result.BotToken != managedToken {
		t.Fatalf("managed setup did not return the correct bot and owner: %v", err)
	}
}

func startUpdate(updateID, senderID, chatID int64, chatType, code string) update {
	return update{
		ID: updateID,
		Message: &message{
			From: &user{ID: senderID}, Chat: chat{ID: chatID, Type: chatType}, Text: "/start " + code,
		},
	}
}

type setupTransport func(*http.Request) any

func (send setupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	defer req.Body.Close()
	jsonBody, err := json.Marshal(map[string]any{"ok": true, "result": send(req)})
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(jsonBody))),
	}, nil
}
