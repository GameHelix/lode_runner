package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/candylario/candylario/trader/internal/hyperliquid"
	"github.com/candylario/candylario/trader/internal/pg"
)

// fakePerps is a perpetuals exchange with one coin, ETH at 2000, that
// fills every market order at 2001 and keeps the stops as resting orders.
type fakePerps struct {
	mu       sync.Mutex
	user     string
	position float64 // ETH held; negative when short
	actions  []map[string]any
	resting  map[int64]bool
	nextOID  int64
	fills    []map[string]any // what userFillsByTime reports
}

// fill adds an execution of an order to what the exchange reports.
func (f *fakePerps) fill(oid int64, dir string, px, sz, pnl float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fills = append(f.fills, map[string]any{"coin": "ETH", "px": fmt.Sprint(px), "sz": fmt.Sprint(sz), "side": "A", "time": time.Now().UnixMilli(),
		"startPosition": "0", "dir": dir, "closedPnl": fmt.Sprint(pnl), "hash": "0x0", "oid": oid, "crossed": true, "fee": "0.01", "tid": len(f.fills) + 1, "feeToken": "USDC"})
}

func (f *fakePerps) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body map[string]json.RawMessage
	json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.URL.Path == "/exchange" {
		var action map[string]any
		json.Unmarshal(body["action"], &action)
		f.actions = append(f.actions, action)
		switch action["type"] {
		case "order":
			var statuses []any
			for i, o := range action["orders"].([]any) {
				order := o.(map[string]any)
				if i == 0 && order["t"].(map[string]any)["limit"] != nil {
					statuses = append(statuses, map[string]any{"filled": map[string]any{"totalSz": order["s"], "avgPx": "2001", "oid": f.oid()}})
					continue
				}
				oid := f.oid()
				f.resting[oid] = true
				statuses = append(statuses, map[string]any{"resting": map[string]any{"oid": oid}})
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"type": "order", "data": map[string]any{"statuses": statuses}}})
		case "cancel":
			for _, c := range action["cancels"].([]any) {
				delete(f.resting, int64(c.(map[string]any)["o"].(float64)))
			}
			json.NewEncoder(w).Encode(map[string]any{"status": "ok", "response": map[string]any{"type": "cancel", "data": map[string]any{"statuses": []string{"success"}}}})
		default:
			w.Write([]byte(`{"status":"ok","response":{"type":"default"}}`))
		}
		return
	}
	switch strings.Trim(string(body["type"]), `"`) {
	case "metaAndAssetCtxs":
		w.Write([]byte(`[{"universe":[{"name":"ETH","szDecimals":4,"maxLeverage":50}]},[{"funding":"0.0001","openInterest":"1000","prevDayPx":"1900","dayNtlVlm":"5e8","markPx":"2000","midPx":"2000","oraclePx":"2000"}]]`))
	case "userRole":
		json.NewEncoder(w).Encode(map[string]any{"role": "agent", "data": map[string]string{"user": f.user}})
	case "maxBuilderFee":
		w.Write([]byte("50"))
	case "clearinghouseState":
		pos := "[]"
		if f.position != 0 {
			pos = fmt.Sprintf(`[{"type":"oneWay","position":{"coin":"ETH","szi":"%v","leverage":{"type":"cross","value":3},"entryPx":"2001","positionValue":"50","unrealizedPnl":"0","liquidationPx":"1500","marginUsed":"17"}}]`, f.position)
		}
		w.Write([]byte(`{"marginSummary":{"accountValue":"1000","totalMarginUsed":"17"},"withdrawable":"900","assetPositions":` + pos + `,"time":1700000000000}`))
	case "userFillsByTime":
		json.NewEncoder(w).Encode(f.fills)
	case "frontendOpenOrders":
		var orders []map[string]any
		for oid := range f.resting {
			orders = append(orders, map[string]any{"coin": "ETH", "oid": oid, "side": "A", "limitPx": "1710", "sz": "0.025", "isTrigger": true, "triggerPx": "1800", "orderType": "Stop Market", "reduceOnly": true})
		}
		json.NewEncoder(w).Encode(orders)
	default:
		http.NotFound(w, r)
	}
}

func (f *fakePerps) oid() int64 {
	f.nextOID++
	return f.nextOID
}

func TestATradeOpensWithItsStopsAndClosesCleanly(t *testing.T) {
	s := service(t)
	fake := &fakePerps{user: "0x5e9ee1089755c3435139848e47e6635505d5a13a", resting: map[int64]bool{}, nextOID: 10}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s.Exchange = func(mode string) *hyperliquid.Client { return &hyperliquid.Client{BaseURL: srv.URL} }
	s.Builder = hyperliquid.Builder{Address: "0xabcdef0123456789abcdef0123456789abcdef01", Fee: 50}
	ctx := context.Background()
	tenant := uuid.New()
	if err := s.Store.CreateTenant(ctx, pg.Tenant{ID: tenant, Name: "acme"}); err != nil {
		t.Fatal(err)
	}
	var refused *Refused
	req := TradeRequest{Coin: "eth", Side: "long", USD: 50, Leverage: 3, StopLossPct: 0.10, TakeProfitPct: 0.20}
	if _, err := s.OpenTrade(ctx, tenant, req); !errors.As(err, &refused) || !strings.Contains(err.Error(), "wallet") {
		t.Fatalf("trade without a wallet: %v", err)
	}
	// The account approves its wallet, as the browser flow does.
	a, err := s.WalletApprovals(ctx, tenant, fake.user, "testnet", 0)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := hyperliquid.NewSigner("0x0123456789012345678901234567890123456789012345678901234567890123")
	raw, _ := json.Marshal(a.ApproveAgent.Action)
	rawB, _ := json.Marshal(a.ApproveBuilderFee.Action)
	sigA, _ := a.ApproveAgent.Sign(account)
	sigB, _ := a.ApproveBuilderFee.Sign(account)
	if _, err := s.ApproveWallet(ctx, tenant, fake.user, "testnet", Signed{Action: raw, Nonce: a.ApproveAgent.Nonce, Signature: sigA}, &Signed{Action: rawB, Nonce: a.ApproveBuilderFee.Nonce, Signature: sigB}); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	fake.actions = nil
	fake.mu.Unlock()

	tr, err := s.OpenTrade(ctx, tenant, req)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Mode != "testnet" || tr.Coin != "ETH" || tr.Side != "long" || tr.Size != 0.025 || tr.EntryPx != 2001 || tr.Leverage != 3 || tr.StopPx != 1800 || tr.TakeProfitPx != 2400 || tr.StopOID == 0 || tr.TakeProfitOID == 0 {
		t.Fatalf("trade %+v", tr)
	}
	if len(fake.actions) != 2 || fake.actions[0]["type"] != "updateLeverage" || fake.actions[0]["leverage"] != float64(3) {
		t.Fatalf("actions %v", fake.actions)
	}
	order := fake.actions[1]
	orders := order["orders"].([]any)
	if order["grouping"] != "normalTpsl" || len(orders) != 3 || order["builder"].(map[string]any)["f"] != float64(50) {
		t.Fatalf("order action %v", order)
	}
	main, stop, tp := orders[0].(map[string]any), orders[1].(map[string]any), orders[2].(map[string]any)
	if main["b"] != true || main["p"] != "2020" || main["s"] != "0.025" || main["t"].(map[string]any)["limit"].(map[string]any)["tif"] != "Ioc" {
		t.Fatalf("main order %v", main)
	}
	if stop["b"] != false || stop["r"] != true || stop["t"].(map[string]any)["trigger"].(map[string]any)["triggerPx"] != "1800" || stop["t"].(map[string]any)["trigger"].(map[string]any)["tpsl"] != "sl" || stop["p"] != "1710" {
		t.Fatalf("stop %v", stop)
	}
	if tp["t"].(map[string]any)["trigger"].(map[string]any)["triggerPx"] != "2400" || tp["t"].(map[string]any)["trigger"].(map[string]any)["tpsl"] != "tp" {
		t.Fatalf("take-profit %v", tp)
	}

	// Limits.
	for _, bad := range []TradeRequest{{Coin: "ETH", Side: "long", USD: 500, Leverage: 3, StopLossPct: 0.1}, {Coin: "ETH", Side: "long", USD: 50, Leverage: 10, StopLossPct: 0.1},
		{Coin: "ETH", Side: "long", USD: 50, Leverage: 3}, {Coin: "DOGE", Side: "short", USD: 50, Leverage: 2, StopLossPct: 0.1}, {Coin: "ETH", Side: "up", USD: 50, Leverage: 2, StopLossPct: 0.1}} {
		if _, err := s.OpenTrade(ctx, tenant, bad); !errors.As(err, &refused) {
			t.Fatalf("%+v: %v", bad, err)
		}
	}

	// Closing sells the position and cancels the two stops.
	fake.mu.Lock()
	fake.position = 0.025
	fake.actions = nil
	fake.mu.Unlock()
	if _, err := s.CloseTrade(ctx, tenant, "SOL", 0); !errors.As(err, &refused) {
		t.Fatalf("close without a position: %v", err)
	}
	c, err := s.CloseTrade(ctx, tenant, "eth", 0)
	if err != nil {
		t.Fatal(err)
	}
	if c.Mode != "testnet" || c.Size != 0.025 || c.AvgPx != 2001 || c.Remaining != 0 || c.StopsCancelled != 2 || len(fake.resting) != 0 {
		t.Fatalf("closed %+v, resting %v", c, fake.resting)
	}
	close := fake.actions[0]["orders"].([]any)[0].(map[string]any)
	if close["b"] != false || close["r"] != true || close["p"] != "1980" || close["s"] != "0.025" {
		t.Fatalf("close order %v", close)
	}
}

func TestFillsAreRecordedAndStopsAnnounced(t *testing.T) {
	s := service(t)
	fake := &fakePerps{user: "0x5e9ee1089755c3435139848e47e6635505d5a13a", resting: map[int64]bool{}, nextOID: 10}
	srv := httptest.NewServer(fake)
	defer srv.Close()
	s.Exchange = func(mode string) *hyperliquid.Client { return &hyperliquid.Client{BaseURL: srv.URL} }
	ctx := context.Background()
	tenant := uuid.New()
	if err := s.Store.CreateTenant(ctx, pg.Tenant{ID: tenant, Name: "acme"}); err != nil {
		t.Fatal(err)
	}
	a, err := s.WalletApprovals(ctx, tenant, fake.user, "testnet", 0)
	if err != nil {
		t.Fatal(err)
	}
	account, _ := hyperliquid.NewSigner("0x0123456789012345678901234567890123456789012345678901234567890123")
	raw, _ := json.Marshal(a.ApproveAgent.Action)
	sig, _ := a.ApproveAgent.Sign(account)
	if _, err := s.ApproveWallet(ctx, tenant, fake.user, "testnet", Signed{Action: raw, Nonce: a.ApproveAgent.Nonce, Signature: sig}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.watch(ctx, tenant); err != nil {
		t.Fatal("an account the engine never traded for is skipped:", err)
	}
	tr, err := s.OpenTrade(ctx, tenant, TradeRequest{Coin: "ETH", Side: "long", USD: 50, Leverage: 2, StopLossPct: 0.10})
	if err != nil {
		t.Fatal(err)
	}
	wake, stop := s.SubscribeNotices()
	defer stop()
	// The open fills, the stop hits while the engine is away, and someone
	// trades on the account elsewhere.
	fake.fill(tr.OID, "Open Long", 2001, 0.025, 0)
	fake.fill(tr.StopOID, "Close Long", 1800, 0.015, -3)
	fake.fill(tr.StopOID, "Close Long", 1800, 0.01, -2)
	fake.fill(999, "Open Short", 1990, 0.01, 0)
	if err := s.watch(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	notices, _ := s.Store.NoticesAfter(ctx, 0, 10)
	var texts []string
	for _, n := range notices {
		texts = append(texts, n.Text)
	}
	if len(texts) != 2 || texts[0] != "🛑 Stop hit: close long 0.025 ETH at $1800 · P&L -$5.02 🧪 testnet" || texts[1] != "Trade on your account: open short 0.01 ETH at $1990 🧪 testnet" {
		t.Fatalf("notices %q", texts)
	}
	select {
	case <-wake:
	default:
		t.Fatal("the notice stream was not woken")
	}
	if err := s.watch(ctx, tenant); err != nil {
		t.Fatal(err)
	}
	if again, _ := s.Store.NoticesAfter(ctx, 0, 10); len(again) != 2 {
		t.Fatalf("a second pass repeats: %d", len(again))
	}

	// Today's loss of $5.02 against a $5 cap: no more trades today.
	var refused *Refused
	if err := s.SetLimits(ctx, tenant, pg.Limits{MaxUSD: 500, MaxLeverage: 3, DailyLossUSD: 5}); !errors.As(err, &refused) {
		t.Fatalf("limits above the engine's caps: %v", err)
	}
	if err := s.SetLimits(ctx, tenant, pg.Limits{MaxUSD: 40, MaxLeverage: 2, DailyLossUSD: 5}); err != nil {
		t.Fatal(err)
	}
	if l, caps, err := s.Limits(ctx, tenant); err != nil || l != (pg.Limits{MaxUSD: 40, MaxLeverage: 2, DailyLossUSD: 5}) || caps.MaxUSD != 100 || caps.DailyLossUSD != 50 {
		t.Fatalf("limits %+v caps %+v %v", l, caps, err)
	}
	if _, err := s.OpenTrade(ctx, tenant, TradeRequest{Coin: "ETH", Side: "long", USD: 45, Leverage: 1, StopLossPct: 0.10}); !errors.As(err, &refused) || !strings.Contains(err.Error(), "$40") {
		t.Fatalf("a trade above the account's own limit: %v", err)
	}
	if _, err := s.OpenTrade(ctx, tenant, TradeRequest{Coin: "ETH", Side: "long", USD: 20, Leverage: 1, StopLossPct: 0.10}); !errors.As(err, &refused) || !strings.Contains(err.Error(), "daily cap") {
		t.Fatalf("a trade past the daily loss cap: %v", err)
	}
}
