package notifier

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/candylario/candylario/notifier/internal/engine"
	"github.com/candylario/candylario/notifier/internal/store"
	"github.com/candylario/candylario/notifier/internal/telegram"
)

// A button trade: this size at this leverage, with the alert's suggested
// stop or this one, and only from an alert this fresh.
const (
	tradeUSD      = 25
	tradeLeverage = 1
	defaultStop   = 10 // percent
	tradeFresh    = 15 * time.Minute
)

// tradeButtons are the buttons under an alert in a connected chat. Each
// carries the trade: "t|long|ETH|8" is long ETH with an 8% stop.
func tradeButtons(a engine.RadarAlert) []telegram.Button {
	stop := defaultStop
	if a.Suggestion != nil && a.Suggestion.StopLossPct > 0 {
		stop = int(math.Round(a.Suggestion.StopLossPct * 100))
	}
	data := func(side string) string { return fmt.Sprintf("t|%s|%s|%d", side, a.Coin, stop) }
	return []telegram.Button{{Text: "Long $25", Data: data("long")}, {Text: "Short $25", Data: data("short")}, {Text: "Ignore", Data: "x"}}
}

func parseTap(data string) (side, coin string, stop float64, ok bool) {
	parts := strings.Split(data, "|")
	if len(parts) != 4 || parts[0] != "t" || (parts[1] != "long" && parts[1] != "short") || parts[2] == "" {
		return "", "", 0, false
	}
	pct, err := strconv.Atoi(parts[3])
	if err != nil || pct <= 0 || pct >= 50 {
		return "", "", 0, false
	}
	return parts[1], parts[2], float64(pct) / 100, true
}

// tap acts on a button under an alert: Long or Short trades it from the
// account's wallet, Ignore takes the buttons away. A message trades once,
// and only while its alert is fresh.
func (s *Service) tap(ctx context.Context, q telegram.Callback) {
	m := q.Message
	if m == nil || q.From == nil || m.Chat.Type != "private" || q.From.ID != m.Chat.ID {
		s.answer(ctx, q.ID, "")
		return
	}
	chat := m.Chat.ID
	if q.Data == "x" {
		s.answer(ctx, q.ID, "")
		s.edit(ctx, chat, m.MessageID, m.Text)
		return
	}
	side, coin, stop, ok := parseTap(q.Data)
	if !ok {
		s.answer(ctx, q.ID, "")
		return
	}
	link, err := s.Store.Link(ctx, chat)
	if errors.Is(err, store.ErrNotLinked) {
		s.answer(ctx, q.ID, "This chat is not connected.")
		return
	}
	if err != nil {
		s.Logf("database: %v", err)
		s.answer(ctx, q.ID, "Something went wrong; try again in a minute.")
		return
	}
	if time.Since(time.Unix(m.Date, 0)) > tradeFresh {
		s.answer(ctx, q.ID, "This alert is too old to trade from.")
		s.edit(ctx, chat, m.MessageID, m.Text)
		return
	}
	if !s.claim(chat, m.MessageID) {
		s.answer(ctx, q.ID, "Already placed.")
		return
	}
	s.answer(ctx, q.ID, "Placing the trade…")
	s.edit(ctx, chat, m.MessageID, m.Text+fmt.Sprintf("\n\n⏳ %s %s $%d…", title(side), coin, tradeUSD))
	go s.trade(ctx, chat, *m, link.Tenant.String(), side, coin, stop)
}

// trade opens the position and writes the outcome under the alert.
func (s *Service) trade(ctx context.Context, chat int64, m telegram.Message, tenant, side, coin string, stop float64) {
	tctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	t, err := s.Engine.OpenTrade(tctx, tenant, coin, side, tradeUSD, tradeLeverage, stop)
	var line string
	var refused *engine.Error
	switch {
	case errors.As(err, &refused) && refused.Code == "refused":
		line = "⚠️ Not placed: " + refused.Message + "." + walletHint(refused.Message)
	case err != nil:
		s.Logf("engine: %v", err)
		line = "⚠️ No answer from the engine; check /positions before trying again."
	default:
		line = fmt.Sprintf("✅ %s %s %s at %s, stop at %s", title(t.Side), num(t.Size), t.Coin, price(t.EntryPx), price(t.StopPx)) + testnet(t.Mode)
	}
	s.edit(ctx, chat, m.MessageID, m.Text+"\n\n"+line)
}

// claim marks a message as traded from, and says whether it was free.
func (s *Service) claim(chat, message int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for k, at := range s.tapped {
		if time.Since(at) > time.Hour {
			delete(s.tapped, k)
		}
	}
	k := [2]int64{chat, message}
	if _, ok := s.tapped[k]; ok {
		return false
	}
	s.tapped[k] = time.Now()
	return true
}

// positions answers /positions with the account and its open positions.
func (s *Service) positions(ctx context.Context, chat int64, tenant string) {
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	w, err := s.Engine.Wallet(cctx, tenant)
	switch {
	case engine.IsCode(err, "not_found"):
		s.say(ctx, chat, "No wallet connected yet. Connect one in the Candylario app.")
		return
	case err != nil:
		s.Logf("engine: %v", err)
		s.say(ctx, chat, "⚠️ The engine did not answer; try again in a minute.")
		return
	case !w.AgentApproved || w.Account == nil:
		s.say(ctx, chat, "Your wallet is not approved yet. Finish connecting it in the Candylario app.")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Account %s · free %s%s", money(w.Account.Value), money(w.Account.Withdrawable), testnet(w.Mode))
	if len(w.Account.Positions) == 0 {
		b.WriteString("\nNo open positions.")
	}
	for _, p := range w.Account.Positions {
		side := "long"
		if p.Size < 0 {
			side = "short"
		}
		pnl := "+" + money(p.UnrealizedPnl)
		if p.UnrealizedPnl < 0 {
			pnl = "-" + money(-p.UnrealizedPnl)
		}
		fmt.Fprintf(&b, "\n%s %s %s at %s · P&L %s", p.Coin, side, num(math.Abs(p.Size)), price(p.EntryPx), pnl)
		if p.LiquidationPx > 0 {
			fmt.Fprintf(&b, " · liquidation %s", price(p.LiquidationPx))
		}
		fmt.Fprintf(&b, " · /close %s", p.Coin)
	}
	s.say(ctx, chat, b.String())
}

// closePosition answers /close COIN.
func (s *Service) closePosition(ctx context.Context, chat int64, tenant, coin string) {
	coin = strings.ToUpper(strings.TrimSpace(coin))
	if coin == "" {
		s.say(ctx, chat, "Which coin? For example /close ETH")
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	c, err := s.Engine.CloseTrade(cctx, tenant, coin)
	var refused *engine.Error
	switch {
	case errors.As(err, &refused) && refused.Code == "refused":
		s.say(ctx, chat, "⚠️ Not closed: "+refused.Message+"."+walletHint(refused.Message))
	case err != nil:
		s.Logf("engine: %v", err)
		s.say(ctx, chat, "⚠️ No answer from the engine; check /positions before trying again.")
	default:
		text := fmt.Sprintf("✅ Closed %s %s at %s", num(c.Size), c.Coin, price(c.AvgPx))
		if c.StopsCancelled > 0 {
			text += fmt.Sprintf("; %d stop orders cancelled", c.StopsCancelled)
		}
		if c.Remaining > 0 {
			text += fmt.Sprintf(". %s did not fill and is still open, guarded by its stops", num(c.Remaining))
		}
		s.say(ctx, chat, text+testnet(c.Mode))
	}
}

func (s *Service) answer(ctx context.Context, callback, text string) {
	if err := s.Bot.Answer(ctx, callback, text); err != nil {
		s.Logf("telegram: %v", err)
	}
}

func (s *Service) edit(ctx context.Context, chat, message int64, text string) {
	if err := s.Bot.Edit(ctx, chat, message, text); err != nil {
		s.Logf("telegram: %v", err)
	}
}

func walletHint(msg string) string {
	if strings.Contains(msg, "wallet") {
		return " Connect it in the Candylario app."
	}
	return ""
}

func testnet(mode string) string {
	if mode == "testnet" {
		return " 🧪 testnet"
	}
	return ""
}

func title(side string) string { return strings.ToUpper(side[:1]) + side[1:] }

// num writes a size without trailing zeros.
func num(x float64) string { return strconv.FormatFloat(x, 'f', -1, 64) }

// price writes a price as the exchange gave it.
func price(x float64) string { return "$" + strconv.FormatFloat(x, 'f', -1, 64) }

// money writes an amount of dollars to the cent.
func money(x float64) string { return fmt.Sprintf("$%.2f", x) }
