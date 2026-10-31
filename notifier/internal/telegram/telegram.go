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
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Date      int64  `json:"date"`
	Text      string `json:"text"`
}

// Callback is a tap on a button under one of the bot's messages.
type Callback struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

type Update struct {
	UpdateID int64     `json:"update_id"`
	Message  *Message  `json:"message"`
	Callback *Callback `json:"callback_query"`
}

// Button is a button under a message; a tap sends Data back as a Callback.
type Button struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
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
	return ups, b.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": int(wait.Seconds()), "allowed_updates": []string{"message", "callback_query"}}, &ups)
}

func cut(text string) string {
	if r := []rune(text); len(r) > 4000 {
		return string(r[:4000]) + "…"
	}
	return text
}

// Send posts plain text to a chat, cut to Telegram's message limit, with a
// row of buttons when given.
func (b *Bot) Send(ctx context.Context, chat int64, text string, buttons ...Button) error {
	params := map[string]any{"chat_id": chat, "text": cut(text), "link_preview_options": map[string]bool{"is_disabled": true}}
	if len(buttons) > 0 {
		params["reply_markup"] = map[string]any{"inline_keyboard": [][]Button{buttons}}
	}
	return b.call(ctx, "sendMessage", params, nil)
}

// Edit replaces a message's text, and with it the buttons it had.
func (b *Bot) Edit(ctx context.Context, chat, message int64, text string) error {
	return b.call(ctx, "editMessageText", map[string]any{"chat_id": chat, "message_id": message, "text": cut(text), "link_preview_options": map[string]bool{"is_disabled": true}}, nil)
}

// Answer ends the spinner on a tapped button, showing text briefly when
// given.
func (b *Bot) Answer(ctx context.Context, callback, text string) error {
	return b.call(ctx, "answerCallbackQuery", map[string]any{"callback_query_id": callback, "text": text}, nil)
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
