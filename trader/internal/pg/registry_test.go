package pg

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jobloom/candylario/trader/internal/testkit"
	"github.com/jobloom/candylario/trader/internal/vault"
)

// testDB returns a fresh, migrated database, or skips the test.
func testDB(t *testing.T) string {
	t.Helper()
	url := testkit.Database(t)
	ctx := context.Background()
	if err := Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(ctx, url); err != nil {
		t.Fatalf("migrations must be idempotent: %v", err)
	}
	return url
}

func TestTenantsAndTheirKeysStayApart(t *testing.T) {
	url := testDB(t)
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, b := Tenant{ID: uuid.New(), Name: "alice"}, Tenant{ID: uuid.New(), Name: "bob"}
	for _, tn := range []Tenant{a, b} {
		if err := s.CreateTenant(ctx, tn); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTenant(ctx, a); !errors.Is(err, ErrExists) {
		t.Fatalf("tenant IDs are unique: %v", err)
	}
	master, _ := vault.NewMasterKey()
	v, _ := vault.New(master)
	id := uuid.New()
	sealed, _ := v.Seal([]byte(id.String()), []byte(`{"key":"k"}`))
	if err := s.PutCredential(ctx, Credential{ID: id, Tenant: a.ID, Kind: "hyperliquid", Label: "wallet", Hint: "0xabc", Sealed: sealed}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutCredential(ctx, Credential{ID: uuid.New(), Tenant: a.ID, Kind: "anthropic", Label: "model", Hint: "…wxyz", Sealed: sealed}); err == nil {
		t.Fatal("only wallets are stored")
	}
	c, err := s.Credential(ctx, a.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if plain, err := v.Open([]byte(id.String()), c.Sealed); err != nil || string(plain) != `{"key":"k"}` {
		t.Fatalf("stored key: %s %v", plain, err)
	}
	if _, err := s.Credential(ctx, b.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another tenant must not see the key: %v", err)
	}
	var none int
	pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return tx.QueryRow(ctx, "select count(*) from credentials").Scan(&none) })
	if none != 0 {
		t.Fatal("without a tenant no keys are visible")
	}

	if err := s.CloseTenant(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if tn, _ := s.Tenant(ctx, a.ID); tn.Status != TenantClosed || tn.Name != "" {
		t.Fatalf("a closed tenant keeps no name: %+v", tn)
	}
	if c, err := s.Credential(ctx, a.ID, id); err != nil || !c.Revoked || c.Hint != "" || len(c.Sealed.Ciphertext) != 0 || len(c.Sealed.WrappedKey) != 0 {
		t.Fatalf("the key is not erased: %+v %v", c, err)
	}
	if err := s.CloseTenant(ctx, a.ID); err != nil {
		t.Fatalf("closing twice: %v", err)
	}
	if err := s.CloseTenant(ctx, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown tenant: %v", err)
	}
	if tn, _ := s.Tenant(ctx, b.ID); tn.Status != "active" || tn.Name != "bob" {
		t.Fatalf("another tenant changed: %+v", tn)
	}
}

// The old agent's keys, tables and functions go, and wallets stay.
func TestTheOldAgentIsGone(t *testing.T) {
	url := testkit.Database(t)
	ctx := context.Background()
	if err := migrate(ctx, url, 6); err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close(ctx)
	tenant, agent := uuid.New(), uuid.New()
	if _, err := owner.Exec(ctx, "select ensure_event_partition('2026-10-30')"); err != nil {
		t.Fatal(err)
	}
	for _, st := range []struct {
		sql  string
		args []any
	}{
		{"insert into tenants (id, name) values ($1, 'alice')", []any{tenant}},
		{"insert into agents (id, tenant_id, name, mode, status) values ($1, $2, 'btc-eth', 'testnet', 'active')", []any{agent, tenant}},
		{"insert into events (id, tenant_id, agent_id, seq, type, at, body, hash) values (gen_random_uuid(), $1, $2, 1, 'notice', '2026-10-30T00:00:00Z', '{}', '\\x00')", []any{tenant, agent}},
	} {
		if _, err := owner.Exec(ctx, st.sql, st.args...); err != nil {
			t.Fatal(st.sql, err)
		}
	}
	for _, kind := range []string{"anthropic", "binance", "hyperliquid"} {
		id := uuid.New()
		if _, err := owner.Exec(ctx, `insert into credentials (id, tenant_id, kind, label, hint, ciphertext, nonce, wrapped_key, key_nonce, master_version)
			values ($1, $2, $3, 'main', '…', '\x01', '\x01', '\x01', '\x01', 1)`, id, tenant, kind); err != nil {
			t.Fatal(err)
		}
		if kind != "hyperliquid" {
			if _, err := owner.Exec(ctx, "insert into agent_credentials (agent_id, tenant_id, kind, credential_id) values ($1, $2, $3, $4)", agent, tenant, kind, id); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	rows, _ := owner.Query(ctx, "select kind from credentials")
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(kinds) != 1 || kinds[0] != "hyperliquid" {
		t.Fatalf("only the wallet is left: %v %v", kinds, err)
	}
	var left int
	owner.QueryRow(ctx, `select count(*) from pg_class where relname in ('agents', 'agent_versions', 'agent_credentials', 'events', 'streams')
		or relname like 'events\_%'`).Scan(&left)
	var fns int
	owner.QueryRow(ctx, "select count(*) from pg_proc where proname in ('runnable_agents', 'ensure_event_partition', 'forbid_change')").Scan(&fns)
	var entitlements int
	owner.QueryRow(ctx, "select count(*) from information_schema.columns where table_name = 'tenants' and column_name = 'entitlements'").Scan(&entitlements)
	if left != 0 || fns != 0 || entitlements != 0 {
		t.Fatalf("left: %d tables, %d functions, %d entitlements columns", left, fns, entitlements)
	}
}

func TestRadarAlertsAreKeptInOrder(t *testing.T) {
	url := testDB(t)
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	at := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	for seq := int64(1); seq <= 5; seq++ {
		a := RadarAlert{Seq: seq, At: at.Add(time.Duration(seq) * time.Minute), Coin: "ETH", Rule: "big move", Alert: json.RawMessage(`{"coin":"ETH","price":2400.5}`)}
		if err := s.AddRadarAlert(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AddRadarAlert(ctx, RadarAlert{Seq: 3, At: at, Coin: "X", Rule: "big move", Alert: json.RawMessage(`{}`)}); !errors.Is(err, ErrExists) {
		t.Fatalf("a number is used once: %v", err)
	}
	after, err := s.RadarAlertsAfter(ctx, 2, 2)
	if err != nil || len(after) != 2 || after[0].Seq != 3 || after[1].Seq != 4 || !after[0].At.Equal(at.Add(3*time.Minute)) || string(after[0].Alert) != `{"coin": "ETH", "price": 2400.5}` {
		t.Fatalf("after: %+v %v", after, err)
	}
	last, err := s.LastRadarAlerts(ctx, 2)
	if err != nil || len(last) != 2 || last[0].Seq != 4 || last[1].Seq != 5 {
		t.Fatalf("last: %+v %v", last, err)
	}
}

func TestTradingRecordsStayWithTheirAccount(t *testing.T) {
	url := testDB(t)
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a, b := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{a, b} {
		if err := s.CreateTenant(ctx, Tenant{ID: id, Name: id.String()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := s.WatchFrom(ctx, a); ok || err != nil {
		t.Fatalf("an account the engine never traded for has nothing to watch: %v %v", ok, err)
	}
	if err := s.AddOrders(ctx, a, []Order{{OID: 1, Kind: OrderOpen, Coin: "ETH"}, {OID: 2, Kind: OrderStop, Coin: "ETH"}}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.WatchFrom(ctx, a); !ok || err != nil {
		t.Fatalf("watched from the first order: %v %v", ok, err)
	}
	at := time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC)
	stop := Fill{TID: 10, OID: 2, At: at, Coin: "ETH", Dir: "Close Long", Size: 0.0125, Price: 1800, ClosedPnl: -2.5, Fee: 0.01}
	for i, want := range []int{1, 0} {
		added, err := s.AddFills(ctx, a, []Fill{stop})
		if err != nil || len(added) != want {
			t.Fatalf("round %d added %v: %v", i, added, err)
		}
	}
	if from, ok, _ := s.WatchFrom(ctx, a); !ok || !from.Equal(at) {
		t.Fatalf("watched from the newest fill: %v", from)
	}
	if pnl, err := s.PnlSince(ctx, a, at.Add(-time.Hour)); err != nil || pnl != -2.51 {
		t.Fatalf("pnl %v %v", pnl, err)
	}
	if o, _ := s.Orders(ctx, a, []int64{2, 99}); len(o) != 1 || o[2].Kind != OrderStop {
		t.Fatalf("orders %+v", o)
	}
	if o, _ := s.Orders(ctx, b, []int64{2}); len(o) != 0 {
		t.Fatalf("another account sees the orders: %+v", o)
	}
	if pnl, _ := s.PnlSince(ctx, b, at.Add(-time.Hour)); pnl != 0 {
		t.Fatalf("another account sees the fills: %v", pnl)
	}
	n1, err := s.AddNotice(ctx, a, "🛑 Stop hit")
	if err != nil {
		t.Fatal(err)
	}
	n2, _ := s.AddNotice(ctx, b, "🎯 Take-profit hit")
	if all, err := s.NoticesAfter(ctx, 0, 10); err != nil || len(all) != 2 || all[0] != n1 || all[1].Tenant != b {
		t.Fatalf("the notifier reads every account's notices in order: %+v %v", all, err)
	}
	if last, err := s.LastNotice(ctx); err != nil || last != n2.Seq {
		t.Fatalf("last notice %d %v", last, err)
	}
	if _, ok, err := s.Limits(ctx, a); ok || err != nil {
		t.Fatalf("no limits set: %v %v", ok, err)
	}
	for _, l := range []Limits{{MaxUSD: 50, MaxLeverage: 3, DailyLossUSD: 20}, {MaxUSD: 25, MaxLeverage: 1, DailyLossUSD: 10}} {
		if err := s.SetLimits(ctx, a, l); err != nil {
			t.Fatal(err)
		}
		if got, ok, err := s.Limits(ctx, a); !ok || err != nil || got != l {
			t.Fatalf("limits %+v %v %v", got, ok, err)
		}
	}
	if _, ok, _ := s.Limits(ctx, b); ok {
		t.Fatal("another account got the limits")
	}
	master, _ := vault.NewMasterKey()
	v, _ := vault.New(master)
	sealed, _ := v.Seal([]byte("x"), []byte(`{"key":"k"}`))
	if err := s.PutCredential(ctx, Credential{ID: uuid.New(), Tenant: b, Kind: "hyperliquid", Label: "wallet", Hint: "0xagent", Sealed: sealed}); err != nil {
		t.Fatal(err)
	}
	if ids, err := s.WalletTenants(ctx); err != nil || len(ids) != 1 || ids[0] != b {
		t.Fatalf("accounts with a wallet: %v %v", ids, err)
	}
}
