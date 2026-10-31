package fleet

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/candylario/candylario/trader/internal/pg"
)

// The engine records each connected account's fills and tells its chats
// what they did: a stop or a take-profit that hit, a liquidation, or a
// trade made on the account elsewhere. Opens and closes made through the
// engine are told when they are made, so they raise no notice.

const defaultDailyLossUSD = 50.0

// caps are the engine's limits; an account's own stay within them.
func (s *Service) caps() pg.Limits {
	maxUSD, maxLev := s.tradeLimits()
	loss := s.TradeMaxDailyLossUSD
	if loss <= 0 {
		loss = defaultDailyLossUSD
	}
	return pg.Limits{MaxUSD: maxUSD, MaxLeverage: maxLev, DailyLossUSD: loss}
}

// Limits returns an account's limits, its own or else the engine's caps,
// and the caps.
func (s *Service) Limits(ctx context.Context, tenant uuid.UUID) (pg.Limits, pg.Limits, error) {
	caps := s.caps()
	own, ok, err := s.Store.Limits(ctx, tenant)
	if err != nil || !ok {
		return caps, caps, err
	}
	own.MaxUSD, own.MaxLeverage, own.DailyLossUSD = min(own.MaxUSD, caps.MaxUSD), min(own.MaxLeverage, caps.MaxLeverage), min(own.DailyLossUSD, caps.DailyLossUSD)
	return own, caps, nil
}

// SetLimits stores an account's own limits, within the engine's caps.
func (s *Service) SetLimits(ctx context.Context, tenant uuid.UUID, l pg.Limits) error {
	if err := s.open(ctx, tenant); err != nil {
		return err
	}
	caps := s.caps()
	switch {
	case math.IsNaN(l.MaxUSD) || l.MaxUSD < minOrderUSD || l.MaxUSD > caps.MaxUSD:
		return refusef("the most per trade must be $%.0f to $%.0f", minOrderUSD, caps.MaxUSD)
	case l.MaxLeverage < 1 || l.MaxLeverage > caps.MaxLeverage:
		return refusef("leverage must be 1 to %d", caps.MaxLeverage)
	case math.IsNaN(l.DailyLossUSD) || l.DailyLossUSD < 1 || l.DailyLossUSD > caps.DailyLossUSD:
		return refusef("the daily loss cap must be $1 to $%.0f", caps.DailyLossUSD)
	}
	return s.Store.SetLimits(ctx, tenant, l)
}

// TodayPnl is what an account made or lost today (UTC) on closed
// positions, net of fees.
func (s *Service) TodayPnl(ctx context.Context, tenant uuid.UUID) (float64, error) {
	return s.Store.PnlSince(ctx, tenant, time.Now().UTC().Truncate(24*time.Hour))
}

// record keeps the orders placed for an account, so its fills can later be
// told apart.
func (s *Service) record(ctx context.Context, tenant uuid.UUID, coin string, orders ...pg.Order) {
	var keep []pg.Order
	for _, o := range orders {
		if o.OID != 0 {
			o.Coin = coin
			keep = append(keep, o)
		}
	}
	if err := s.Store.AddOrders(context.WithoutCancel(ctx), tenant, keep); err != nil {
		s.logf("orders of %s: %v", tenant, err)
	}
}

func (s *Service) logf(format string, args ...any) {
	if s.Logf != nil {
		s.Logf(format, args...)
	}
}

// Watch reads every connected account's new fills each interval until ctx
// ends. An account is read from its newest stored fill, so fills that
// happened while the engine was down are recorded when it starts again.
func (s *Service) Watch(ctx context.Context, every time.Duration) error {
	for {
		tenants, err := s.Store.WalletTenants(ctx)
		if err != nil && ctx.Err() == nil {
			s.logf("fills: %v", err)
		}
		for _, t := range tenants {
			if err := s.watch(ctx, t); err != nil && ctx.Err() == nil {
				s.logf("fills of %s: %v", t, err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

func (s *Service) watch(ctx context.Context, tenant uuid.UUID) error {
	_, sec, ok, err := s.wallet(ctx, tenant)
	if err != nil || !ok || sec.User == "" {
		return err
	}
	from, ok, err := s.Store.WatchFrom(ctx, tenant)
	if err != nil || !ok {
		return err
	}
	ex, err := s.exchange(sec.Mode)
	if err != nil {
		return err
	}
	fills, err := ex.Fills(ctx, sec.User, from)
	if err != nil {
		return err
	}
	rows := make([]pg.Fill, len(fills))
	for i, f := range fills {
		rows[i] = pg.Fill{TID: f.TID, OID: f.OID, At: f.Time, Coin: f.Coin, Dir: f.Dir, Buy: f.Buy, Size: f.Size, Price: f.Price, ClosedPnl: f.ClosedPnl, Fee: f.Fee}
	}
	added, err := s.Store.AddFills(ctx, tenant, rows)
	if err != nil || len(added) == 0 {
		return err
	}
	var oids []int64
	byOrder := map[int64][]pg.Fill{}
	for _, f := range added {
		if _, seen := byOrder[f.OID]; !seen {
			oids = append(oids, f.OID)
		}
		byOrder[f.OID] = append(byOrder[f.OID], f)
	}
	orders, err := s.Store.Orders(ctx, tenant, oids)
	if err != nil {
		return err
	}
	for _, oid := range oids {
		o, ours := orders[oid]
		text := noticeText(byOrder[oid], o, ours, sec.Mode)
		if text == "" {
			continue
		}
		if _, err := s.Store.AddNotice(ctx, tenant, text); err != nil {
			return err
		}
		s.wakeNotices()
	}
	return nil
}

// noticeText tells an account's chats what one order's new fills did; ""
// for an open or a close the engine made, which was told already.
func noticeText(fills []pg.Fill, o pg.Order, ours bool, mode string) string {
	if ours && (o.Kind == pg.OrderOpen || o.Kind == pg.OrderClose) {
		return ""
	}
	var size, value, pnl float64
	for _, f := range fills {
		size, value, pnl = size+f.Size, value+f.Size*f.Price, pnl+f.ClosedPnl-f.Fee
	}
	f := fills[0]
	head := "Trade on your account"
	switch {
	case ours && o.Kind == pg.OrderStop:
		head = "🛑 Stop hit"
	case ours && o.Kind == pg.OrderTakeProfit:
		head = "🎯 Take-profit hit"
	case strings.Contains(f.Dir, "Liquidat"):
		head = "⚠️ Liquidated"
	}
	text := fmt.Sprintf("%s: %s %s %s at $%s", head, strings.ToLower(f.Dir), trim(size, 8), f.Coin, trim(value/size, priceDecimals(value/size)))
	if !strings.HasPrefix(f.Dir, "Open") {
		sign := "+"
		if pnl < 0 {
			sign = "-"
		}
		text += fmt.Sprintf(" · P&L %s$%.2f", sign, math.Abs(pnl))
	}
	if mode == ModeTestnet {
		text += " 🧪 testnet"
	}
	return text
}

// priceDecimals keeps cents from a dollar up and six significant digits
// below.
func priceDecimals(x float64) int {
	if x >= 1 || x <= 0 {
		return 2
	}
	return 5 - int(math.Floor(math.Log10(x)))
}

// trim rounds to decimals and drops trailing zeros.
func trim(x float64, decimals int) string {
	p := math.Pow(10, float64(decimals))
	return strconv.FormatFloat(math.Round(x*p)/p, 'f', -1, 64)
}

// SubscribeNotices returns a channel that gets a value when a notice is
// added, and a function to stop.
func (s *Service) SubscribeNotices() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.subs == nil {
		s.subs = map[chan struct{}]bool{}
	}
	s.subs[ch] = true
	return ch, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.subs, ch)
	}
}

func (s *Service) wakeNotices() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
