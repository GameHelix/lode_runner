package main

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/candylario/candylario/trader/internal/fleet"
	"github.com/candylario/candylario/trader/internal/hyperliquid"
	"github.com/candylario/candylario/trader/internal/pg"
	"github.com/candylario/candylario/trader/internal/radar"
	"github.com/candylario/candylario/trader/internal/server"
	"github.com/candylario/candylario/trader/internal/vault"
)

// openFleet connects to the database and the vault.
func openFleet(ctx context.Context, e env) (*fleet.Service, func(), error) {
	url, master := os.Getenv("DATABASE_URL"), os.Getenv("ENGINE_MASTER_KEY")
	if url == "" || master == "" {
		return nil, nil, errors.New("set DATABASE_URL and ENGINE_MASTER_KEY")
	}
	// Older master keys, kept while stored keys are re-wrapped under the
	// current one.
	var previous []string
	for _, k := range strings.Split(os.Getenv("ENGINE_MASTER_KEY_PREVIOUS"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			previous = append(previous, k)
		}
	}
	v, err := vault.New(master, previous...)
	if err != nil {
		return nil, nil, err
	}
	if err := pg.Migrate(ctx, url); err != nil {
		return nil, nil, err
	}
	st, err := pg.Open(ctx, url)
	if err != nil {
		return nil, nil, err
	}
	svc := &fleet.Service{Store: st, Vault: v}
	svc.Exchange = func(mode string) *hyperliquid.Client {
		url := hyperliquid.TestnetURL
		if mode == fleet.ModeMainnet {
			url = hyperliquid.MainnetURL
		}
		return &hyperliquid.Client{HTTP: e.HTTP, BaseURL: url, Now: e.Now}
	}
	fee, err := strconv.Atoi(cmp.Or(os.Getenv("HYPERLIQUID_BUILDER_FEE"), "0"))
	if err != nil || fee < 0 || fee > 1000 {
		return nil, nil, errors.New("HYPERLIQUID_BUILDER_FEE is the fee in tenths of a basis point, 0 to 1000")
	}
	svc.Builder = hyperliquid.Builder{Address: strings.ToLower(os.Getenv("HYPERLIQUID_BUILDER")), Fee: fee}
	if v := os.Getenv("TRADE_MAX_USD"); v != "" {
		if svc.TradeMaxUSD, err = strconv.ParseFloat(v, 64); err != nil || svc.TradeMaxUSD < 10 {
			return nil, nil, errors.New("TRADE_MAX_USD is the largest position in dollars, at least 10")
		}
	}
	if v := os.Getenv("TRADE_MAX_LEVERAGE"); v != "" {
		if svc.TradeMaxLeverage, err = strconv.Atoi(v); err != nil || svc.TradeMaxLeverage < 1 || svc.TradeMaxLeverage > 50 {
			return nil, nil, errors.New("TRADE_MAX_LEVERAGE is 1 to 50")
		}
	}
	if v := os.Getenv("TRADE_MAX_DAILY_LOSS_USD"); v != "" {
		if svc.TradeMaxDailyLossUSD, err = strconv.ParseFloat(v, 64); err != nil || svc.TradeMaxDailyLossUSD < 1 {
			return nil, nil, errors.New("TRADE_MAX_DAILY_LOSS_USD is the most an account may lose in a day, in dollars, at least 1")
		}
	}
	return svc, st.Close, nil
}

// serve runs the radar and the engine API until ctx ends.
func serve(ctx context.Context, svc *fleet.Service, e env, out io.Writer) error {
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "%s %s\n", time.Now().UTC().Format("2006-01-02 15:04:05Z"), fmt.Sprintf(format, args...))
	}
	svc.Logf = logf
	rl, sampled, err := startRadar(ctx, e, svc.Store, logf)
	if err != nil {
		return err
	}
	startWatch(ctx, svc, logf)
	done, err := startAPI(ctx, svc, rl, sampled, logf)
	if err != nil {
		return err
	}
	<-ctx.Done()
	<-done
	return nil
}

// startWatch records every connected account's fills and the notices they
// raise, every 30 seconds. A failure ends only that run; it starts again a
// minute later.
func startWatch(ctx context.Context, svc *fleet.Service, logf func(string, ...any)) {
	go func() {
		for ctx.Err() == nil {
			func() {
				defer func() {
					if p := recover(); p != nil {
						logf("fills panic: %v", p)
					}
				}()
				if err := svc.Watch(ctx, 30*time.Second); err != nil && ctx.Err() == nil {
					logf("fills: %v", err)
				}
			}()
			select {
			case <-ctx.Done():
			case <-time.After(time.Minute):
			}
		}
	}()
}

// startAPI opens the engine API, for the app, when ENGINE_API_TOKEN is set.
// ENGINE_NOTIFIER_TOKEN, when set, is a second token for the notifier that
// opens only the routes it needs.
// It returns a channel closed once the API has stopped.
func startAPI(ctx context.Context, svc *fleet.Service, rl *radar.Log, sampled func() time.Time, logf func(string, ...any)) (<-chan struct{}, error) {
	done := make(chan struct{})
	token, notifier := os.Getenv("ENGINE_API_TOKEN"), os.Getenv("ENGINE_NOTIFIER_TOKEN")
	if token == "" {
		logf("engine API off; set ENGINE_API_TOKEN to open it")
		close(done)
		return done, nil
	}
	if len(token) < 32 {
		return nil, errors.New("ENGINE_API_TOKEN must be at least 32 characters")
	}
	if notifier != "" && (len(notifier) < 32 || notifier == token) {
		return nil, errors.New("ENGINE_NOTIFIER_TOKEN must be at least 32 characters and differ from ENGINE_API_TOKEN")
	}
	l, err := net.Listen("tcp", cmp.Or(os.Getenv("ENGINE_API_ADDR"), ":8080"))
	if err != nil {
		return nil, fmt.Errorf("engine API: %w", err)
	}
	srv := &server.Server{Fleet: svc, Radar: rl, RadarSampled: sampled, Token: token, NotifierToken: notifier, Commit: commit, Logf: logf}
	go func() {
		defer close(done)
		if err := srv.Serve(ctx, l); err != nil && ctx.Err() == nil {
			logf("engine API stopped: %v", err)
		}
	}()
	logf("engine API on %s", l.Addr())
	return done, nil
}
