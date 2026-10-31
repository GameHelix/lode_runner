// Package telegram calls the Telegram Bot API for one bot.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const DefaultURL = "https://api.telegram.org"

type Bot struct {
	HTTP    *http.Client
	BaseURL string
	Token   string
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type Message struct {
	From *User  `json:"from"`
	Chat Chat   `json:"chat"`
	Date int64  `json:"date"`
	Text string `json:"text"`
}

type Update struct {
	UpdateID int64    `json:"update_id"`
	Message  *Message `json:"message"`
}

// Error is a call Telegram refused. RetryAfter is set when it asks to slow
// down.
type Error struct {
	Method      string
	Code        int
	Description string
	RetryAfter  time.Duration
}

func (e *Error) Error() string {
	return fmt.Sprintf("telegram %s: %d %s", e.Method, e.Code, e.Description)
}

func (b *Bot) call(ctx context.Context, method string, params, out any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	base := b.BaseURL
	if base == "" {
		base = DefaultURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/bot"+b.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return errors.New("telegram: invalid request")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := b.HTTP.Do(req)
	if err != nil {
		// The URL holds the token, so only the cause is reported.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	defer resp.Body.Close()
	var env struct {
		OK          bool            `json:"ok"`
		Code        int             `json:"error_code"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
		Parameters  struct {
			RetryAfter int `json:"retry_after"`
		} `json:"parameters"`
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 5<<20))
	if err != nil {
		return fmt.Errorf("telegram %s: %w", method, err)
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return &Error{Method: method, Code: resp.StatusCode, Description: http.StatusText(resp.StatusCode)}
	}
	if !env.OK {
		return &Error{Method: method, Code: env.Code, Description: env.Description, RetryAfter: time.Duration(env.Parameters.RetryAfter) * time.Second}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(env.Result, out)
}

func (b *Bot) Me(ctx context.Context) (User, error) {
	var u User
	return u, b.call(ctx, "getMe", struct{}{}, &u)
}

// Updates waits up to wait for messages from offset on.
func (b *Bot) Updates(ctx context.Context, offset int64, wait time.Duration) ([]Update, error) {
	ctx, cancel := context.WithTimeout(ctx, wait+20*time.Second)
	defer cancel()
	var ups []Update
	return ups, b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": int(wait.Seconds()), "allowed_updates": []string{"message"}}, &ups)
}

// Send posts plain text to a chat, cut to Telegram's message limit.
func (b *Bot) Send(ctx context.Context, chat int64, text string) error {
	if r := []rune(text); len(r) > 4000 {
		text = string(r[:4000]) + "…"
	}
	return b.call(ctx, "sendMessage", map[string]any{"chat_id": chat, "text": text, "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}

// Command is an entry in the bot's command menu.
type Command struct {
	Command     string `json:"command"`
	Description string `json:"description"`
}

// SetProfile sets the bot's command menu and the texts people see before
// they start it.
func (b *Bot) SetProfile(ctx context.Context, commands []Command, description, short string) error {
	return errors.Join(
		b.call(ctx, "setMyCommands", map[string]any{"commands": commands}, nil),
		b.call(ctx, "setMyDescription", map[string]string{"description": description}, nil),
		b.call(ctx, "setMyShortDescription", map[string]string{"short_description": short}, nil),
	)
}
