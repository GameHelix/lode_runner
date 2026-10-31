package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/candylario/candylario/trader/internal/hyperliquid"
	"github.com/candylario/candylario/trader/internal/pg"
	"github.com/candylario/candylario/trader/internal/radar"
)

// runRadar watches every perpetual and prints each unusual move as it
// happens. It needs no keys.
func runRadar(ctx context.Context, args []string, out io.Writer, e env) error {
	fs := flag.NewFlagSet("radar", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	interval := fs.Duration("interval", 30*time.Second, "time between samples")
	asJSON := fs.Bool("json", false, "print each alert as a JSON line")
	testnet := fs.Bool("testnet", false, "watch the testnet instead of the live exchange")
	noWarm := fs.Bool("no-warmup", false, "skip loading the past day of candles")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(out, usage)
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected arguments; run `trader help`")
	}
	url := hyperliquid.MainnetURL
	if *testnet {
		url = hyperliquid.TestnetURL
	}
	client := &hyperliquid.Client{HTTP: e.HTTP, BaseURL: url, Now: e.Now}
	enc := json.NewEncoder(out)
	w := &radar.Watcher{
		Source:   client,
		Books:    client,
		Interval: *interval,
		Now:      e.Now,
		Logf:     func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) },
		Alert: func(a radar.Alert) {
			if *asJSON {
				enc.Encode(a)
				return
			}
			fmt.Fprintf(out, "%s  %s\n", a.At.UTC().Format("15:04"), a)
		},
	}
	fmt.Fprintf(out, "radar on %s, a sample every %s\n", url, *interval)
	if !*noWarm {
		if err := w.Warm(ctx, client); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			fmt.Fprintf(out, "no warm-up: %v; alerts start after an hour of samples\n", err)
		}
	}
	return quiet(w.Run(ctx))
}

// quiet turns the end of a run by Ctrl-C into a clean exit.
func quiet(err error) error {
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// startRadar runs the radar beside the engine API and keeps its alerts for the
// API, and in Postgres, so a restart serves them, numbers on and keeps
// their cooldowns. HYPERLIQUID_URL picks the exchange; the live one by
// default. A failure ends only that run; the radar starts again a minute
// later.
func startRadar(ctx context.Context, e env, store *pg.Store, logf func(string, ...any)) (*radar.Log, error) {
	client := &hyperliquid.Client{HTTP: e.HTTP, BaseURL: cmp.Or(os.Getenv("HYPERLIQUID_URL"), hyperliquid.MainnetURL), Now: e.Now}
	rl := &radar.Log{Store: radarStore{store}, Logf: logf}
	kept, err := store.LastRadarAlerts(ctx, 1000)
	if err != nil {
		return nil, fmt.Errorf("radar alerts: %w", err)
	}
	restored, err := radarEntries(kept)
	if err != nil {
		return nil, err
	}
	rl.Restore(restored)
	w := &radar.Watcher{Source: client, Books: client, Now: e.Now, Logf: logf, Alert: func(a radar.Alert) {
		rl.Add(a)
		logf("radar: %s", a)
	}}
	go func() {
		for ctx.Err() == nil {
			func() {
				defer func() {
					if p := recover(); p != nil {
						logf("radar panic: %v", p)
					}
				}()
				if err := w.Warm(ctx, client); err != nil && ctx.Err() == nil {
					logf("radar warm-up: %v", err)
				}
				var recent []radar.Alert
				for _, en := range rl.Last(1000) {
					recent = append(recent, en.Alert)
				}
				w.Recall(recent)
				if err := w.Run(ctx); err != nil && ctx.Err() == nil {
					logf("radar: %v", err)
				}
			}()
			select {
			case <-ctx.Done():
			case <-time.After(time.Minute):
			}
		}
	}()
	logf("radar on %s, %d alerts kept, numbering from %d", client.BaseURL, len(restored), lastSeq(restored))
	return rl, nil
}

func lastSeq(entries []radar.Entry) int64 {
	if len(entries) == 0 {
		return 0
	}
	return entries[len(entries)-1].Seq
}

// radarStore keeps the radar's alerts in the engine's database.
type radarStore struct{ store *pg.Store }

func (s radarStore) SaveAlert(e radar.Entry) error {
	raw, err := json.Marshal(e.Alert)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return s.store.AddRadarAlert(ctx, pg.RadarAlert{Seq: e.Seq, At: e.At, Coin: e.Coin, Rule: e.Rule, Alert: raw})
}

func (s radarStore) AlertsAfter(seq int64, limit int) ([]radar.Entry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	rows, err := s.store.RadarAlertsAfter(ctx, seq, limit)
	if err != nil {
		return nil, err
	}
	return radarEntries(rows)
}

func radarEntries(rows []pg.RadarAlert) ([]radar.Entry, error) {
	out := make([]radar.Entry, len(rows))
	for i, r := range rows {
		out[i].Seq = r.Seq
		if err := json.Unmarshal(r.Alert, &out[i].Alert); err != nil {
			return nil, fmt.Errorf("radar alert %d: %w", r.Seq, err)
		}
	}
	return out, nil
}
