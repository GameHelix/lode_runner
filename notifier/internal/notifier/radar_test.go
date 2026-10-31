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
	"github.com/candylario/candylario/notifier/internal/telegram"
)

// fakeRadar is the engine's radar API: the latest alerts and their stream.
type fakeRadar struct {
	mu         sync.Mutex
	alerts     []engine.RadarAlert
	generation int // bumped when the engine "restarts": streams end, numbering starts over
}

func (f *fakeRadar) add(text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, engine.RadarAlert{Seq: int64(len(f.alerts) + 1), At: time.Now(), Coin: "X", Text: text})
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
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func TestRadarAlertsReachTheChannelOnceAndRestartsRepeatNothing(t *testing.T) {
	tg := &fakeTelegram{sent: map[int64][]string{}}
	radar := &fakeRadar{}
	radar.add("🚀 OLD +5.0% in 15m")
	radar.add("🔻 OLDER -4.0% in 15m")
	const channel = -1001234
	s := &Service{
		Bot:    &telegram.Bot{BaseURL: tg.server(t).URL, Token: "t", HTTP: http.DefaultClient},
		Engine: &engine.Client{BaseURL: radar.server(t).URL, Token: "n", HTTP: http.DefaultClient},
		Logf:   t.Logf, RadarChannel: channel, Retry: 20 * time.Millisecond, Gap: time.Millisecond, Rate: 1000,
	}
	s.defaults()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.deliver(ctx)
	go s.radar(ctx)

	wait := func(what string, ok func() bool) {
		t.Helper()
		for start := time.Now(); !ok(); {
			if time.Since(start) > 3*time.Second {
				t.Fatalf("waiting for %s; channel got %q", what, tg.texts(channel))
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if got := tg.texts(channel); len(got) != 0 {
		t.Fatalf("alerts from before the start were posted: %q", got)
	}
	radar.add("🚀 HYPE +5.2% in 15m, +7.0% in 1h, +12.9% today · 6× the usual volume · $81.40")
	wait("the new alert", func() bool { return len(tg.texts(channel)) == 1 })
	if got := tg.texts(channel)[0]; got != "🚀 HYPE +5.2% in 15m, +7.0% in 1h, +12.9% today · 6× the usual volume · $81.40" {
		t.Fatalf("posted %q", got)
	}

	// The engine restarts: its stream drops and its numbering starts over.
	// The alert it raised before the notifier reconnected is not repeated
	// as news; the next one arrives.
	radar.restart()
	radar.add("🚀 WHILE-AWAY +9.0% in 15m")
	time.Sleep(150 * time.Millisecond)
	radar.add("🔻 SOL -4.1% in 15m, -2.0% in 1h, +1.0% today · open interest -12.0% in 1h · $180.20")
	wait("the alert after the restart", func() bool { return len(tg.texts(channel)) == 2 })
	if got := tg.texts(channel); got[1] != "🔻 SOL -4.1% in 15m, -2.0% in 1h, +1.0% today · open interest -12.0% in 1h · $180.20" {
		t.Fatalf("posted %q", got)
	}
	time.Sleep(100 * time.Millisecond)
	if got := tg.texts(channel); len(got) != 2 {
		t.Fatalf("repeated something: %q", got)
	}
}
