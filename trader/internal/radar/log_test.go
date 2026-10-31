package radar

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// memStore is a store that keeps every alert, or refuses to.
type memStore struct {
	mu      sync.Mutex
	entries []Entry
	broken  bool
}

func (m *memStore) SaveAlert(e Entry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.broken {
		return errors.New("database down")
	}
	m.entries = append(m.entries, e)
	return nil
}

func (m *memStore) AlertsAfter(seq int64, limit int) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Entry
	for _, e := range m.entries {
		if e.Seq > seq && len(out) < limit {
			out = append(out, e)
		}
	}
	return out, nil
}

func TestTheLogRestoresSavesAndReadsOlderAlertsFromTheStore(t *testing.T) {
	store := &memStore{}
	at := time.Date(2026, 10, 31, 8, 0, 0, 0, time.UTC)
	var kept []Entry
	for seq := int64(1); seq <= 1500; seq++ {
		kept = append(kept, Entry{Seq: seq, Alert: Alert{At: at, Coin: fmt.Sprint("C", seq)}})
	}
	store.entries = append(store.entries, kept...)
	var logged []string
	l := &Log{Store: store, Logf: func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }}
	l.Restore(kept)

	if e := l.Add(Alert{Coin: "NEW"}); e.Seq != 1501 {
		t.Fatalf("numbering goes on after the restored alerts: %d", e.Seq)
	}
	if n := len(store.entries); n != 1501 || store.entries[n-1].Coin != "NEW" {
		t.Fatalf("the new alert is saved: %d", n)
	}
	if last := l.Last(2); len(last) != 2 || last[0].Seq != 1500 || last[1].Seq != 1501 {
		t.Fatalf("last %+v", last)
	}
	if older := l.After(10, 3); len(older) != 3 || older[0].Seq != 11 || older[2].Seq != 13 {
		t.Fatalf("alerts older than memory come from the store: %+v", older)
	}
	if recent := l.After(1499, 5); len(recent) != 2 || recent[0].Seq != 1500 {
		t.Fatalf("recent alerts come from memory: %+v", recent)
	}
	store.broken = true
	if e := l.Add(Alert{Coin: "LOST"}); e.Seq != 1502 || len(logged) != 1 || !strings.Contains(logged[0], "alert 1502 not saved") {
		t.Fatalf("a failed save is reported and the alert still served: %d %q", e.Seq, logged)
	}
}

func TestRecalledAlertsKeepTheirCooldowns(t *testing.T) {
	w := &Watcher{}
	start := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	coins := map[string]*market{"BTC": {mid: 80000, oi: 40000, dayVol: 2e9}, "ETH": {mid: 2400, oi: 1e6, dayVol: 5e8}}
	at := quiet(w, start, 3, coins)
	// Before the restart ETH was alerted ten minutes ago and BTC's funding
	// flipped two hours ago.
	w.Recall([]Alert{
		{At: at.Add(-10 * time.Minute), Coin: "ETH", Rule: RuleBigMove},
		{At: at.Add(-2 * time.Hour), Coin: "BTC", Rule: RuleFundingFlip},
		{At: at.Add(-3 * time.Hour), Coin: "ETH", Rule: RuleBigHour},
		{At: at.Add(-time.Hour), Coin: "SOL", Rule: RuleMomentum},
	})
	if tr := w.coins["ETH"]; !tr.lastAlert.Equal(at.Add(-10 * time.Minute)) {
		t.Fatalf("the newest alert sets the cooldown: %v", tr.lastAlert)
	}
	if tr := w.coins["SOL"]; !tr.momentumAt.Equal(at.Add(-time.Hour)) || !tr.lastAlert.Equal(at.Add(-time.Hour)) {
		t.Fatalf("a momentum run holds both cooldowns: %+v", tr)
	}
	// ETH jumps 5% on volume inside its hour: no alert.
	for i := 0; i < 30; i++ {
		coins["ETH"].mid *= 1 + 0.05/30
		coins["ETH"].dayVol += 6 * coins["ETH"].dayVol / 96 / 30
		if alerts := feed(w, at, coins); len(alerts) != 0 {
			t.Fatalf("alerted inside a recalled cooldown: %s", alerts[0])
		}
		at = at.Add(30 * time.Second)
	}
	// BTC's funding turns very negative: no second flip within the day.
	tr := w.coins["BTC"]
	tr.samples[len(tr.samples)-1].funding = -0.0004
	if _, ok := tr.fundingFlip(at, "BTC"); ok {
		t.Fatal("a funding flip came again within the day")
	}
	if _, ok := tr.fundingFlip(at.Add(23*time.Hour), "BTC"); !ok {
		t.Fatal("the funding flip did not come back after a day")
	}
}
