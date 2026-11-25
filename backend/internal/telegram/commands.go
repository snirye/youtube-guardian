package telegram

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/snirye/youtube-guardian/backend/internal/store"
)

func (b *Bot) execute(ctx context.Context, command string, args []string) (string, error) {
	var text strings.Builder
	switch command {
	case "/start", "/help":
		text.WriteString("YouTube Guardian\n")
		for _, c := range commands {
			fmt.Fprintf(&text, "/%s — %s\n", c.Name, c.Description)
		}
	case "/status":
		o, err := b.data.LastHeartbeat(ctx)
		if err != nil {
			return "", err
		}
		text.WriteString("Backend is running.\n")
		if o == nil {
			text.WriteString("Chrome extension has not been observed. Chrome or the computer may have been inactive.")
		} else if time.Since(o.At) > 2*time.Minute {
			fmt.Fprintf(&text, "Extension not recently observed (last seen %s). Chrome or the computer may have been inactive.",
				o.At.UTC().Format(time.RFC3339))
		} else {
			fmt.Fprintf(&text, "Extension %s recently active (%s).", o.ExtensionVersion, o.At.UTC().Format(time.RFC3339))
			if o.Version.Valid {
				fmt.Fprintf(&text, "\nConfiguration version %d.", o.Version.Int64)
			}
			if o.Tabs.Valid {
				fmt.Fprintf(&text, "\n%d YouTube tab(s).", o.Tabs.Int64)
			}
		}
	case "/pending":
		entries, err := b.data.Pending(ctx, false, 15)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "There are no pending requests.", nil
		}
		for _, r := range entries {
			fmt.Fprintln(&text, formatRequest(r)+"\n")
		}
	case "/allow", "/block":
		if len(args) == 0 || !store.ValidChannel(args[0]) {
			return "Usage: " + command + " UC… [channel name]", nil
		}
		name := strings.Join(args[1:], " ")
		if len(name) > 320 {
			return "The channel name is too long.", nil
		}
		allow := command == "/allow"
		if err := b.data.Decide(ctx, args[0], name, allow); err != nil {
			return "", err
		}
		if allow {
			return "Channel approved.", nil
		}
		return "Channel blocked.", nil
	case "/channels":
		s, err := b.data.Snapshot(ctx)
		if err != nil {
			return "", err
		}
		for _, c := range s.Channels {
			if c.Decision == "approved" {
				fmt.Fprintf(&text, "%s (%s)\n", c.Name, c.ID)
			}
		}
		if text.Len() == 0 {
			return "There are no approved channels.", nil
		}
	case "/logs":
		entries, err := b.data.Recent(ctx, 15)
		if err != nil {
			return "", err
		}
		if len(entries) == 0 {
			return "There is no recent activity.", nil
		}
		for _, e := range entries {
			fmt.Fprintf(&text, "%s %s %s %s %s %s %s",
				e.At.UTC().Format(time.RFC3339), e.Type, e.Decision, e.ChannelName, e.ChannelID, e.VideoID, e.RequestStatus)
			if e.Version.Valid {
				fmt.Fprintf(&text, " v%d", e.Version.Int64)
			}
			text.WriteByte('\n')
		}
	case "/shorts_on", "/shorts_off":
		enable := command == "/shorts_on"
		if err := b.data.SetShorts(ctx, enable); err != nil {
			return "", err
		}
		if enable {
			return "Shorts are hidden and blocked.", nil
		}
		return "Shorts follow the channel approval rules.", nil
	default:
		return "Unrecognized command. Use /help.", nil
	}
	return strings.TrimSpace(text.String()), nil
}
