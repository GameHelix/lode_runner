package pg

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Kinds of orders the engine places for an account.
const (
	OrderOpen       = "open"
	OrderStop       = "stop"
	OrderTakeProfit = "take_profit"
	OrderClose      = "close"
)

// Order is an order the engine placed for an account.
type Order struct {
	OID  int64
	Kind string
	Coin string
	At   time.Time
}

// AddOrders records orders the engine placed; one recorded before stays.
func (s *Store) AddOrders(ctx context.Context, tenant uuid.UUID, orders []Order) error {
	return s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		for _, o := range orders {
			if _, err := tx.Exec(ctx, "insert into orders (tenant_id, oid, kind, coin) values ($1, $2, $3, $4) on conflict do nothing", tenant, o.OID, o.Kind, o.Coin); err != nil {
				return err
			}
		}
		return nil
	})
}

// Orders returns the orders among oids that the engine placed, by oid.
func (s *Store) Orders(ctx context.Context, tenant uuid.UUID, oids []int64) (map[int64]Order, error) {
	out := map[int64]Order{}
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "select oid, kind, coin, at from orders where oid = any($1)", oids)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var o Order
			if err := rows.Scan(&o.OID, &o.Kind, &o.Coin, &o.At); err != nil {
				return err
			}
			out[o.OID] = o
		}
		return rows.Err()
	})
	return out, err
}

// Fill is one execution on an account, as the exchange reported it.
type Fill struct {
	TID       int64
	OID       int64
	At        time.Time
	Coin      string
	Dir       string // Open Long, Close Short, ...
	Buy       bool
	Size      float64
	Price     float64
	ClosedPnl float64
	Fee       float64
}

// AddFills stores the fills not stored before and returns them, in order.
func (s *Store) AddFills(ctx context.Context, tenant uuid.UUID, fills []Fill) ([]Fill, error) {
	var added []Fill
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		added = nil
		for _, f := range fills {
			tag, err := tx.Exec(ctx, `insert into fills (tenant_id, tid, oid, at, coin, dir, buy, size, price, closed_pnl, fee)
				values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) on conflict do nothing`,
				tenant, f.TID, f.OID, f.At, f.Coin, f.Dir, f.Buy, f.Size, f.Price, f.ClosedPnl, f.Fee)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				added = append(added, f)
			}
		}
		return nil
	})
	return added, err
}

// WatchFrom is when to read an account's fills from: its newest fill, or
// its first order when it has no fill yet. ok is false when the engine has
// never traded for it.
func (s *Store) WatchFrom(ctx context.Context, tenant uuid.UUID) (time.Time, bool, error) {
	var at *time.Time
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select coalesce((select max(at) from fills), (select min(at) from orders))").Scan(&at)
	})
	if err != nil || at == nil {
		return time.Time{}, false, err
	}
	return at.UTC(), true, nil
}

// PnlSince is what an account made or lost on closed positions since a
// time, net of fees.
func (s *Store) PnlSince(ctx context.Context, tenant uuid.UUID, since time.Time) (float64, error) {
	var pnl float64
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select coalesce(sum(closed_pnl - fee), 0) from fills where at >= $1", since).Scan(&pnl)
	})
	return pnl, err
}

// Notice is something an account's chats are told, in one stream across
// accounts.
type Notice struct {
	Seq    int64
	Tenant uuid.UUID
	At     time.Time
	Text   string
}

func (s *Store) AddNotice(ctx context.Context, tenant uuid.UUID, text string) (Notice, error) {
	n := Notice{Tenant: tenant, Text: text}
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "insert into notices (tenant_id, text) values ($1, $2) returning seq, at", tenant, text).Scan(&n.Seq, &n.At)
	})
	n.At = n.At.UTC()
	return n, err
}

// NoticesAfter returns up to limit notices of every account numbered above
// seq, in order.
func (s *Store) NoticesAfter(ctx context.Context, seq int64, limit int) ([]Notice, error) {
	rows, err := s.pool.Query(ctx, "select seq, tenant_id, at, text from notices_after($1, $2)", seq, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notice, error) {
		var n Notice
		err := r.Scan(&n.Seq, &n.Tenant, &n.At, &n.Text)
		n.At = n.At.UTC()
		return n, err
	})
}

// LastNotice is the number of the newest notice, 0 for none.
func (s *Store) LastNotice(ctx context.Context) (int64, error) {
	var seq int64
	return seq, s.pool.QueryRow(ctx, "select last_notice()").Scan(&seq)
}

// WalletTenants lists the active accounts that have a wallet.
func (s *Store) WalletTenants(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := s.pool.Query(ctx, "select wallet_tenants()")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

// Limits are what an account allows itself on each trade and each day.
type Limits struct {
	MaxUSD       float64
	MaxLeverage  int
	DailyLossUSD float64
}

// Limits returns the account's own limits; ok is false when it has set
// none.
func (s *Store) Limits(ctx context.Context, tenant uuid.UUID) (Limits, bool, error) {
	var l Limits
	err := s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "select max_usd, max_leverage, daily_loss_usd from limits where tenant_id = $1", tenant).Scan(&l.MaxUSD, &l.MaxLeverage, &l.DailyLossUSD)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

func (s *Store) SetLimits(ctx context.Context, tenant uuid.UUID, l Limits) error {
	return s.inTenant(ctx, tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into limits (tenant_id, max_usd, max_leverage, daily_loss_usd) values ($1, $2, $3, $4)
			on conflict (tenant_id) do update set max_usd = excluded.max_usd, max_leverage = excluded.max_leverage, daily_loss_usd = excluded.daily_loss_usd`,
			tenant, l.MaxUSD, l.MaxLeverage, l.DailyLossUSD)
		return err
	})
}
