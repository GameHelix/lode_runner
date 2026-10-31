package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/candylario/candylario/notifier/internal/engine"
	"github.com/candylario/candylario/notifier/internal/store"
	"github.com/candylario/candylario/notifier/internal/telegram"
)

// fakeRadar is the engine's radar API: the latest alerts and their stream.
type fakeRadar struct {
	mu         sync.Mutex
	alerts     []engine.RadarAlert
	generation int          // bumped when the engine "restarts": streams end, numbering starts over
	tenants    http.Handler // answers the rest of the engine's routes, when set
}

func (f *fakeRadar) add(text string) { f.addAt(text, time.Now()) }

func (f *fakeRadar) addAt(text string, at time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, engine.RadarAlert{Seq: int64(len(f.alerts) + 1), At: at, Coin: "X", Text: text})
}

func (f *fakeRadar) restart() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts, f.generation = nil, f.generation+1
}

func (f *fakeRadar) server(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/radar/alerts":
			f.mu.Lock()
			var last []engine.RadarAlert
			if n := len(f.alerts); n > 0 {
				last = f.alerts[n-1:]
			}
			f.mu.Unlock()
			json.NewEncoder(w).Encode(map[string]any{"alerts": last})
		case "/v1/radar/stream":
			after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			rc := http.NewResponseController(w)
			f.mu.Lock()
			gen := f.generation
			f.mu.Unlock()
			for r.Context().Err() == nil {
				f.mu.Lock()
				if f.generation != gen {
					f.mu.Unlock()
					return
				}
				for _, a := range f.alerts {
					if a.Seq > after {
						raw, _ := json.Marshal(a)
						fmt.Fprintf(w, "id: %d\nevent: radar.alert\ndata: %s\n\n", a.Seq, raw)
						after = a.Seq
					}
				}
				f.mu.Unlock()
				rc.Flush()
				time.Sleep(10 * time.Millisecond)
			}
		default:
			if f.tenants != nil {
				f.tenants.ServeHTTP(w, r)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// alert raises an alert with a suggestion, as the engine does.
func (f *fakeRadar) alert(coin, text, side string, stop float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a := engine.RadarAlert{Seq: int64(len(f.alerts) + 1), At: time.Now(), Coin: coin, Text: text}
	if side != "" {
		a.Suggestion = &engine.Suggestion{Side: side, StopLossPct: stop}
	}
	f.alerts = append(f.alerts, a)
}

func TestRadarAlertsReachTheChannelOnceAcrossRestarts(t *testing.T) {
	st, err := store.Open(context.Background(), testDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	tg := &fakeTelegram{sent: map[int64][]string{}}
	radar := &fakeRadar{}
	radar.add("🚀 OLD +5.0% in 15m")
	radar.add("🔻 OLDER -4.0% in 15m")
	const channel = -1001234
	tgURL, radarURL := tg.server(t).URL, radar.server(t).URL
	start := func() func() {
		s := &Service{
			Bot:    &telegram.Bot{BaseURL: tgURL, Token: "t", HTTP: http.DefaultClient},
			Engine: &engine.Client{BaseURL: radarURL, Token: "n", HTTP: http.DefaultClient},
			Store:  st, Logf: t.Logf, RadarChannel: channel, Retry: 20 * time.Millisecond, Gap: time.Millisecond, Rate: 1000,
		}
		s.defaults()
		ctx, cancel := context.WithCancel(context.Background())
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); s.deliver(ctx) }()
		go func() { defer wg.Done(); s.radar(ctx) }()
		return func() { cancel(); wg.Wait() }
	}
	wait := func(what string, n int) {
		t.Helper()
		for begin := time.Now(); len(tg.texts(channel)) != n; {
			if time.Since(begin) > 3*time.Second {
				t.Fatalf("waiting for %s; channel got %q", what, tg.texts(channel))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	stop := start()
	time.Sleep(100 * time.Millisecond)
	if got := tg.texts(channel); len(got) != 0 {
		t.Fatalf("alerts from before the first start were posted: %q", got)
	}
	radar.add("🚀 HYPE +5.2% in 15m · $81.40")
	wait("the new alert", 1)

	// The notifier restarts. A fresh alert raised meanwhile goes out; a
	// stale one is skipped; nothing repeats.
	stop()
	radar.addAt("🔻 STALE -6.0% in 15m", time.Now().Add(-time.Hour))
	radar.add("🚀 WHILE-DOWN +7.0% in 15m")
	stop = start()
	wait("the alert raised while the notifier was down", 2)
	if got := tg.texts(channel); got[1] != "🚀 WHILE-DOWN +7.0% in 15m" {
		t.Fatalf("posted %q", got)
	}
	if cur, ok, err := st.RadarCursor(context.Background(), channel); err != nil || !ok || cur != 5 {
		t.Fatalf("cursor %d %v %v", cur, ok, err)
	}

	// The engine starts its numbering over: its stream drops, and the next
	// alert still arrives.
	radar.restart()
	radar.add("🚀 WHILE-AWAY +9.0% in 15m")
	time.Sleep(150 * time.Millisecond)
	radar.add("🔻 SOL -4.1% in 15m · $180.20")
	wait("the alert after the engine started over", 3)
	if got := tg.texts(channel); got[2] != "🔻 SOL -4.1% in 15m · $180.20" {
		t.Fatalf("posted %q", got)
	}
	stop()
	time.Sleep(50 * time.Millisecond)
	if got := tg.texts(channel); len(got) != 3 {
		t.Fatalf("repeated something: %q", got)
	}
}
