// Package notifier runs the Candylario bot. It posts the engine's radar
// alerts to the radar channel, in order, with retries, and never twice, and
// connects private chats to accounts with one-time links from the app.
package notifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/candylario/candylario/notifier/internal/engine"
	"github.com/candylario/candylario/notifier/internal/store"
	"github.com/candylario/candylario/notifier/internal/telegram"
)

type Service struct {
	Bot    *telegram.Bot
	Engine *engine.Client
	Store  *store.Store
	Logf   func(format string, args ...any)

	// Retry is how long a dropped stream waits, Gap the time between two
	// messages to one chat, and Rate the messages per second overall,
	// under Telegram's 30.
	Retry time.Duration
	Gap   time.Duration
	Rate  int
	// RadarChannel, when set, is a chat that gets every radar alert.
	RadarChannel int64

	name string
	out  chan outgoing
}

type outgoing struct {
	chat int64
	text string
	done chan struct{}
}

const helpText = `This bot posts the Candylario radar: unusual moves on Hyperliquid perpetuals, each with what followed such moves before.
/disconnect – stop updates in this chat`

var menu = []telegram.Command{
	{Command: "help", Description: "What I can do"},
	{Command: "disconnect", Description: "Stop updates in this chat"},
}

const (
	description      = "The Candylario radar: unusual moves on Hyperliquid perpetuals as they happen, each with the record of what followed such moves before. Connect this chat from the Candylario app."
	shortDescription = "Unusual moves on Hyperliquid perpetuals, as they happen."
)

func (s *Service) defaults() {
	if s.Retry == 0 {
		s.Retry = 5 * time.Second
	}
	if s.Gap == 0 {
		s.Gap = time.Second
	}
	if s.Rate == 0 {
		s.Rate = 25
	}
	s.out = make(chan outgoing, 1000)
}

// Run serves until ctx ends.
func (s *Service) Run(ctx context.Context) error {
	s.defaults()
	me, err := s.Bot.Me(ctx)
	if err != nil {
		return fmt.Errorf("telegram bot: %w", err)
	}
	s.name = me.Username
	if err := s.Bot.SetProfile(ctx, menu, description, shortDescription); err != nil {
		s.Logf("bot profile: %v", err)
	}
	go s.deliver(ctx)
	if s.RadarChannel != 0 {
		go s.radar(ctx)
	}
	s.Logf("notifier running as @%s", s.name)
	return s.listen(ctx)
}

func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

// listen reads the bot's messages and handles each once, across restarts.
func (s *Service) listen(ctx context.Context) error {
	offset, err := s.Store.Offset(ctx, s.name)
	if err != nil {
		return err
	}
	for ctx.Err() == nil {
		ups, err := s.Bot.Updates(ctx, offset, 50*time.Second)
		if err != nil {
			if ctx.Err() == nil {
				s.Logf("telegram: %v", err)
				sleep(ctx, 5*time.Second)
			}
			continue
		}
		for _, u := range ups {
			if u.Message != nil {
				s.handle(ctx, *u.Message)
			}
			offset = u.UpdateID + 1
			if err := s.Store.SetOffset(ctx, s.name, offset); err != nil {
				s.Logf("database: %v", err)
			}
		}
	}
	return nil
}

// say queues a message without waiting for it to go out.
func (s *Service) say(ctx context.Context, chat int64, text string) {
	select {
	case s.out <- outgoing{chat: chat, text: text}:
	case <-ctx.Done():
	}
}

// send queues a message and waits until it is delivered or dropped.
func (s *Service) send(ctx context.Context, chat int64, text string) error {
	done := make(chan struct{})
	select {
	case s.out <- outgoing{chat: chat, text: text, done: done}:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// deliver sends the queue in order, within Telegram's limits overall and
// per chat.
func (s *Service) deliver(ctx context.Context) {
	last := map[int64]time.Time{}
	pace := time.NewTicker(time.Second / time.Duration(s.Rate))
	defer pace.Stop()
	for {
		var m outgoing
		select {
		case <-ctx.Done():
			return
		case m = <-s.out:
		}
		select {
		case <-ctx.Done():
			return
		case <-pace.C:
		}
		if wait := s.Gap - time.Since(last[m.chat]); wait > 0 && !sleep(ctx, wait) {
			return
		}
		s.post(ctx, m)
		last[m.chat] = time.Now()
		if m.done != nil {
			close(m.done)
		}
	}
}

// post sends one message. It waits as long as Telegram asks and retries
// failures that may pass; a message Telegram refuses, such as to a chat
// that blocked the bot, is dropped.
func (s *Service) post(ctx context.Context, m outgoing) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := s.Bot.Send(ctx, m.chat, m.text)
		var te *telegram.Error
		switch {
		case err == nil:
			return
		case errors.As(err, &te) && te.RetryAfter > 0:
			sleep(ctx, te.RetryAfter)
		case errors.As(err, &te) && te.Code >= 400 && te.Code < 500:
			s.Logf("telegram: chat %d: %v; message dropped", m.chat, err)
			return
		default:
			s.Logf("telegram: %v; retrying", err)
			sleep(ctx, backoff)
			backoff = min(2*backoff, time.Minute)
		}
	}
}

// handle answers one message from a private chat with its owner; group
// messages are ignored.
func (s *Service) handle(ctx context.Context, m telegram.Message) {
	if m.Chat.Type != "private" || m.From == nil || m.From.ID != m.Chat.ID {
		return
	}
	chat := m.Chat.ID
	text := strings.TrimSpace(m.Text)
	if !strings.HasPrefix(text, "/") {
		s.say(ctx, chat, helpText)
		return
	}
	name, arg, _ := strings.Cut(text[1:], " ")
	name, _, _ = strings.Cut(name, "@")
	name, arg = strings.ToLower(name), strings.TrimSpace(arg)
	if name == "start" {
		s.start(ctx, m, arg)
		return
	}
	_, err := s.Store.Link(ctx, chat)
	if errors.Is(err, store.ErrNotLinked) {
		s.say(ctx, chat, "This chat is not connected. Open the link from the Candylario app.")
		return
	}
	if err != nil {
		s.Logf("database: %v", err)
		s.say(ctx, chat, "⚠️ Something went wrong; try again in a minute.")
		return
	}
	switch name {
	case "disconnect":
		if err := s.Store.Unlink(ctx, chat); err != nil {
			s.Logf("database: %v", err)
			s.say(ctx, chat, "⚠️ Something went wrong; try again in a minute.")
			return
		}
		s.say(ctx, chat, "Disconnected. Nothing more will arrive here. Open a new link from the Candylario app to connect again.")
	default:
		s.say(ctx, chat, helpText)
	}
}

func (s *Service) start(ctx context.Context, m telegram.Message, code string) {
	chat := m.Chat.ID
	if code == "" {
		if _, err := s.Store.Link(ctx, chat); err == nil {
			s.say(ctx, chat, "This chat is connected.\n\n"+helpText)
		} else {
			s.say(ctx, chat, "Open the link from the Candylario app to connect this chat.")
		}
		return
	}
	name := m.From.FirstName
	if m.From.Username != "" {
		name = "@" + m.From.Username
	}
	l, err := s.Store.Redeem(ctx, code, chat, name)
	if errors.Is(err, store.ErrBadCode) {
		s.say(ctx, chat, "This link has expired or was already used. Get a new one from the Candylario app.")
		return
	}
	if err != nil {
		s.Logf("database: %v", err)
		s.say(ctx, chat, "⚠️ Something went wrong; try again in a minute.")
		return
	}
	s.Logf("chat %d connected to tenant %s", chat, l.Tenant)
	s.say(ctx, chat, "✅ Connected.\n\n"+helpText)
}
