package notifier

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeTenants is the engine's wallet and trade routes for every tenant:
// trades fill at 2001 with the stop the request asked for.
type fakeTenants struct {
	mu      sync.Mutex
	trades  []map[string]any
	closes  []string
	refuse  string // when set, trades are refused with this message
	notices []map[string]any
}

// notice records a notice about an account's trading, as the engine does.
func (f *fakeTenants) notice(tenant uuid.UUID, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.notices = append(f.notices, map[string]any{"seq": len(f.notices) + 1, "tenant_id": tenant.String(), "at": time.Now(), "text": text})
}

func (f *fakeTenants) handler() http.Handler {
	mux := http.NewServeMux()
	reply := func(w http.ResponseWriter, status int, v any) {
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("POST /v1/tenants/{t}/trades", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.refuse != "" {
			reply(w, 422, map[string]any{"error": map[string]string{"code": "refused", "message": f.refuse}})
			return
		}
		in["tenant"] = r.PathValue("t")
		f.trades = append(f.trades, in)
		stop := 2001 * (1 + in["stop_loss_pct"].(float64))
		if in["side"] == "long" {
			stop = 2001 * (1 - in["stop_loss_pct"].(float64))
		}
		reply(w, 200, map[string]any{"mode": "testnet", "coin": in["coin"], "side": in["side"], "size": 0.0125, "entry_px": 2001, "usd": 25, "leverage": 1, "stop_px": stop})
	})
	mux.HandleFunc("POST /v1/tenants/{t}/trades/close", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.closes = append(f.closes, in["coin"])
		if in["coin"] != "ETH" {
			reply(w, 422, map[string]any{"error": map[string]string{"code": "refused", "message": "no position in " + in["coin"]}})
			return
		}
		reply(w, 200, map[string]any{"mode": "testnet", "coin": "ETH", "size": 0.0125, "avg_px": 1990, "remaining": 0, "stops_cancelled": 1})
	})
	mux.HandleFunc("GET /v1/notices", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		last := []map[string]any{}
		if n := len(f.notices); n > 0 {
			last = f.notices[n-1:]
		}
		reply(w, 200, map[string]any{"notices": last})
	})
	mux.HandleFunc("GET /v1/notices/stream", func(w http.ResponseWriter, r *http.Request) {
		after, _ := strconv.Atoi(r.URL.Query().Get("after"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		rc := http.NewResponseController(w)
		for r.Context().Err() == nil {
			f.mu.Lock()
			for _, n := range f.notices {
				if seq := n["seq"].(int); seq > after {
					raw, _ := json.Marshal(n)
					fmt.Fprintf(w, "id: %d\nevent: notice\ndata: %s\n\n", seq, raw)
					after = seq
				}
			}
			f.mu.Unlock()
			rc.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	})
	mux.HandleFunc("GET /v1/tenants/{t}/wallet", func(w http.ResponseWriter, r *http.Request) {
		reply(w, 200, map[string]any{"agent_approved": true, "mode": "testnet", "account": map[string]any{"value": 1500.5, "withdrawable": 1200,
			"positions": []map[string]any{{"coin": "ETH", "size": -0.0125, "entry_px": 2001, "unrealized_pnl": 0.14, "liquidation_px": 3900, "leverage": 1}}}})
	})
	return mux
}

func (f *fakeTenants) placed() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.trades)
}

func TestAlertsTradeFromAConnectedChat(t *testing.T) {
	r := newRig(t)
	tenant := uuid.New()
	r.link(42, tenant)
	r.radar.alert("ETH", "🫂 CROWDED RISE — ETH  $2001", "short", 0.08)
	r.waitFor("the alert with buttons", func() bool { return len(r.tg.buttoned(42)) == 1 })
	m := r.tg.buttoned(42)[0]
	if m.text != "🫂 CROWDED RISE — ETH  $2001" || !slices.Equal(m.buttons, []string{"t|long|ETH|8", "t|short|ETH|8", "x"}) {
		t.Fatalf("alert %+v", m)
	}

	// Short: one trade at the alert's stop, written under the alert.
	r.tg.tap(m, "t|short|ETH|8", time.Now())
	r.waitFor("the fill", func() bool { return strings.Contains(r.tg.edited(m.id), "✅") })
	if got := r.tg.edited(m.id); got != m.text+"\n\n✅ Short 0.0125 ETH at $2001, stop at $2161.08 🧪 testnet" {
		t.Fatalf("edited %q", got)
	}
	trades := r.eng.placed()
	if len(trades) != 1 || trades[0]["tenant"] != tenant.String() || trades[0]["coin"] != "ETH" || trades[0]["side"] != "short" || trades[0]["usd"] != float64(25) || trades[0]["leverage"] != float64(1) || trades[0]["stop_loss_pct"] != 0.08 {
		t.Fatalf("trades %v", trades)
	}
	// A second tap on the same alert trades nothing.
	r.tg.tap(m, "t|long|ETH|8", time.Now())
	r.waitFor("the refusal", func() bool { return slices.Contains(r.tg.answered(), "Already placed.") })

	// An old alert, Ignore, and a refused trade.
	r.radar.alert("SOL", "⚡ BIG MOVE — SOL  $180", "", 0)
	r.waitFor("the second alert", func() bool { return len(r.tg.buttoned(42)) == 2 })
	sol := r.tg.buttoned(42)[1]
	if !slices.Equal(sol.buttons, []string{"t|long|SOL|10", "t|short|SOL|10", "x"}) {
		t.Fatalf("an alert without a suggestion stops at 10%%: %v", sol.buttons)
	}
	r.tg.tap(sol, "t|long|SOL|10", time.Now().Add(-20*time.Minute))
	r.waitFor("too old", func() bool { return slices.Contains(r.tg.answered(), "This alert is too old to trade from.") })
	r.tg.tap(sol, "x", time.Now())
	r.waitFor("the buttons to go", func() bool { return r.tg.edited(sol.id) == sol.text })
	r.radar.alert("HYPE", "🚨 MOMENTUM RUN — HYPE  $40", "long", 0.05)
	r.waitFor("the third alert", func() bool { return len(r.tg.buttoned(42)) == 3 })
	hype := r.tg.buttoned(42)[2]
	r.eng.mu.Lock()
	r.eng.refuse = "the account has no wallet yet"
	r.eng.mu.Unlock()
	r.tg.tap(hype, "t|long|HYPE|5", time.Now())
	r.waitFor("the refusal under the alert", func() bool { return strings.Contains(r.tg.edited(hype.id), "⚠️") })
	if got := r.tg.edited(hype.id); !strings.HasSuffix(got, "⚠️ Not placed: the account has no wallet yet. Connect it in the Candylario app.") {
		t.Fatalf("refused %q", got)
	}
	if n := len(r.eng.placed()); n != 1 {
		t.Fatalf("%d trades, want the first only", n)
	}

	// The positions and a close.
	r.tg.message(42, "/positions", time.Now())
	r.waitFor("the positions", func() bool { return r.said(42, "Account $1500.50") })
	if !r.said(42, "ETH short 0.0125 at $2001 · P&L +$0.14 · liquidation $3900 · /close ETH") || !r.said(42, "🧪 testnet") {
		t.Fatalf("positions %q", r.tg.texts(42))
	}
	r.tg.message(42, "/close eth", time.Now())
	r.waitFor("the close", func() bool { return r.said(42, "✅ Closed 0.0125 ETH at $1990; 1 stop orders cancelled 🧪 testnet") })
	r.tg.message(42, "/close SOL", time.Now())
	r.waitFor("a refused close", func() bool { return r.said(42, "⚠️ Not closed: no position in SOL.") })
	r.tg.message(42, "/close ETH", time.Now().Add(-time.Hour))
	r.waitFor("a late close to be ignored", func() bool { return r.said(42, "⚠️ Ignored /close") })
	r.eng.mu.Lock()
	closes := slices.Clone(r.eng.closes)
	r.eng.mu.Unlock()
	if !slices.Equal(closes, []string{"ETH", "SOL"}) {
		t.Fatalf("closes %v", closes)
	}
}

func TestNoticesReachTheirAccountsChatsOnce(t *testing.T) {
	r := newRig(t)
	alice, bob := uuid.New(), uuid.New()
	r.link(42, alice)
	r.link(43, bob)
	r.eng.notice(alice, "🛑 Stop hit: close long 0.025 ETH at $1800 · P&L -$5.02 🧪 testnet")
	r.waitFor("alice's notice", func() bool { return r.said(42, "🛑 Stop hit") })
	r.stop()
	r.eng.notice(bob, "🎯 Take-profit hit: close short 0.01 SOL at $150 · P&L +$3.00")
	r.start()
	r.waitFor("bob's notice, raised while the notifier was down", func() bool { return r.said(43, "🎯 Take-profit hit") })
	time.Sleep(100 * time.Millisecond)
	if r.count(42, "Stop hit") != 1 || r.said(42, "Take-profit") || r.said(43, "Stop hit") {
		t.Fatalf("alice %q, bob %q", r.tg.texts(42), r.tg.texts(43))
	}
}
