package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/candylario/candylario/notifier/internal/engine"
	"github.com/candylario/candylario/notifier/internal/store"
	"github.com/candylario/candylario/notifier/internal/telegram"
)

// testDatabase creates an empty, migrated database on the server in
// TEST_DATABASE_URL, or skips the test.
func testDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("set TEST_DATABASE_URL to run the notifier tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := "notifier_test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, err := pgx.Connect(context.Background(), base); err == nil {
			c.Exec(context.Background(), "drop database "+name+" with (force)")
			c.Close(context.Background())
		}
	})
	i := strings.LastIndex(base, "/")
	url := base[:i+1] + name
	if q := strings.Index(base[i:], "?"); q >= 0 {
		url += base[i+q:]
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("migrations run once: %v", err)
	}
	return url
}

// fakeTelegram is the Bot API for one bot and its chats.
type fakeTelegram struct {
	mu      sync.Mutex
	updates []telegram.Update
	sent    map[int64][]string
	slow    int // sends to answer with "too many requests" first
	msgs    []sentMessage
	edits   map[int64]string // the newest text of each edited message
	answers []string
}

// sentMessage is a message the bot sent, with its buttons' data.
type sentMessage struct {
	id      int64
	chat    int64
	text    string
	buttons []string
}

func (f *fakeTelegram) server(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		f.mu.Lock()
		defer f.mu.Unlock()
		switch method {
		case "getMe":
			io.WriteString(w, `{"ok":true,"result":{"id":1,"username":"TestBot"}}`)
		case "getUpdates":
			offset := int64(body["offset"].(float64))
			var out []telegram.Update
			for _, u := range f.updates {
				if u.UpdateID >= offset {
					out = append(out, u)
				}
			}
			if len(out) == 0 {
				f.mu.Unlock()
				time.Sleep(20 * time.Millisecond)
				f.mu.Lock()
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": out})
		case "sendMessage":
			if f.slow > 0 {
				f.slow--
				io.WriteString(w, `{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`)
				return
			}
			chat := int64(body["chat_id"].(float64))
			f.sent[chat] = append(f.sent[chat], body["text"].(string))
			m := sentMessage{id: int64(len(f.msgs) + 1), chat: chat, text: body["text"].(string)}
			if rm, ok := body["reply_markup"].(map[string]any); ok {
				for _, row := range rm["inline_keyboard"].([]any) {
					for _, b := range row.([]any) {
						m.buttons = append(m.buttons, b.(map[string]any)["callback_data"].(string))
					}
				}
			}
			f.msgs = append(f.msgs, m)
			fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, m.id)
		case "editMessageText":
			if f.edits == nil {
				f.edits = map[int64]string{}
			}
			f.edits[int64(body["message_id"].(float64))] = body["text"].(string)
			io.WriteString(w, `{"ok":true,"result":true}`)
		case "answerCallbackQuery":
			f.answers = append(f.answers, body["text"].(string))
			io.WriteString(w, `{"ok":true,"result":true}`)
		default:
			io.WriteString(w, `{"ok":true,"result":true}`)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// message queues a message from a person in their private chat.
func (f *fakeTelegram) message(chat int64, text string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, telegram.Update{UpdateID: int64(len(f.updates) + 1), Message: &telegram.Message{
		From: &telegram.User{ID: chat, Username: "trader" + strconv.FormatInt(chat, 10)}, Chat: telegram.Chat{ID: chat, Type: "private"}, Date: at.Unix(), Text: text}})
}

func (f *fakeTelegram) group(chat, from int64, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, telegram.Update{UpdateID: int64(len(f.updates) + 1), Message: &telegram.Message{
		From: &telegram.User{ID: from}, Chat: telegram.Chat{ID: chat, Type: "group"}, Date: time.Now().Unix(), Text: text}})
}

// tap queues a tap on a button of a message the bot sent at the given time.
func (f *fakeTelegram) tap(m sentMessage, data string, sent time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	msg := &telegram.Message{MessageID: m.id, Chat: telegram.Chat{ID: m.chat, Type: "private"}, Date: sent.Unix(), Text: m.text}
	f.updates = append(f.updates, telegram.Update{UpdateID: int64(len(f.updates) + 1), Callback: &telegram.Callback{
		ID: fmt.Sprint("cb", len(f.updates)), From: &telegram.User{ID: m.chat}, Message: msg, Data: data}})
}

// buttoned returns the messages to the chat that have buttons.
func (f *fakeTelegram) buttoned(chat int64) []sentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sentMessage
	for _, m := range f.msgs {
		if m.chat == chat && len(m.buttons) > 0 {
			out = append(out, m)
		}
	}
	return out
}

func (f *fakeTelegram) edited(id int64) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.edits[id]
}

func (f *fakeTelegram) answered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.answers)
}

func (f *fakeTelegram) texts(chat int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.sent[chat])
}

type rig struct {
	t      *testing.T
	store  *store.Store
	tg     *fakeTelegram
	radar  *fakeRadar
	eng    *fakeTenants
	tgURL  string
	engURL string
	stop   func()
}

func newRig(t *testing.T) *rig {
	st, err := store.Open(context.Background(), testDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	r := &rig{t: t, store: st, tg: &fakeTelegram{sent: map[int64][]string{}}, eng: &fakeTenants{}}
	r.radar = &fakeRadar{tenants: r.eng.handler()}
	r.tgURL, r.engURL = r.tg.server(t).URL, r.radar.server(t).URL
	r.start()
	t.Cleanup(func() { r.stop() })
	return r
}

// start runs a notifier, as a restart would.
func (r *rig) start() {
	svc := &Service{Store: r.store, Logf: r.t.Logf, Retry: 50 * time.Millisecond, Gap: time.Millisecond, Rate: 1000,
		Bot:    &telegram.Bot{HTTP: http.DefaultClient, BaseURL: r.tgURL, Token: "bot-token"},
		Engine: &engine.Client{BaseURL: r.engURL, Token: "engine-token", HTTP: http.DefaultClient}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.Run(ctx); close(done) }()
	r.stop = func() { cancel(); <-done; time.Sleep(20 * time.Millisecond) }
}

func (r *rig) waitFor(what string, ok func() bool) {
	r.t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s", what)
		}
	}
}

// said reports whether the chat got a message containing substr.
func (r *rig) said(chat int64, substr string) bool {
	return slices.ContainsFunc(r.tg.texts(chat), func(m string) bool { return strings.Contains(m, substr) })
}

func (r *rig) count(chat int64, substr string) int {
	n := 0
	for _, m := range r.tg.texts(chat) {
		if strings.Contains(m, substr) {
			n++
		}
	}
	return n
}

// link connects a chat to the tenant through a one-time code.
func (r *rig) link(chat int64, tenant uuid.UUID) string {
	code, err := r.store.NewCode(context.Background(), tenant, time.Minute)
	if err != nil {
		r.t.Fatal(err)
	}
	r.tg.message(chat, "/start "+code, time.Now())
	r.waitFor("the chat to connect", func() bool { return r.said(chat, "✅ Connected") })
	return code
}

func TestAChatConnectsAndDisconnects(t *testing.T) {
	r := newRig(t)
	tenant := uuid.New()
	r.tg.message(42, "/help", time.Now())
	r.waitFor("the refusal", func() bool { return r.said(42, "This chat is not connected") })
	r.tg.group(-5, 42, "/start abc")
	code := r.link(42, tenant)
	if l, err := r.store.Link(context.Background(), 42); err != nil || l.Tenant != tenant || l.Name != "@trader42" {
		t.Fatalf("link %+v %v", l, err)
	}
	r.tg.message(43, "/start "+code, time.Now())
	r.waitFor("a used code to be refused", func() bool { return r.said(43, "expired or was already used") })
	r.tg.message(42, "hello", time.Now())
	r.waitFor("the help", func() bool { return r.count(42, "/disconnect") >= 2 })
	r.tg.message(42, "/disconnect", time.Now())
	r.waitFor("the goodbye", func() bool { return r.said(42, "Disconnected") })
	if _, err := r.store.Link(context.Background(), 42); !errors.Is(err, store.ErrNotLinked) {
		t.Fatalf("still linked: %v", err)
	}
	if got := r.tg.texts(-5); len(got) != 0 {
		t.Fatalf("a group was answered: %q", got)
	}
}

func TestRestartsAnswerEachMessageOnce(t *testing.T) {
	r := newRig(t)
	r.link(42, uuid.New())
	r.stop()
	r.tg.message(42, "/disconnect", time.Now())
	r.start()
	r.waitFor("the missed message", func() bool { return r.said(42, "Disconnected") })
	r.stop()
	r.start()
	time.Sleep(200 * time.Millisecond)
	if r.count(42, "Disconnected") != 1 || r.count(42, "✅ Connected") != 1 {
		t.Fatalf("after a restart nothing repeats: %q", r.tg.texts(42))
	}
}

func TestTelegramAskingToSlowDownDelaysButKeepsMessages(t *testing.T) {
	r := newRig(t)
	r.link(42, uuid.New())
	r.tg.mu.Lock()
	r.tg.slow = 1
	r.tg.mu.Unlock()
	r.tg.message(42, "/start", time.Now())
	r.tg.message(42, "/disconnect", time.Now())
	r.waitFor("both replies", func() bool { return r.said(42, "Disconnected") })
	if got := r.tg.texts(42); !strings.HasPrefix(got[len(got)-2], "This chat is connected") || !strings.HasPrefix(got[len(got)-1], "Disconnected") {
		t.Fatalf("in order, none lost: %q", got)
	}
}
