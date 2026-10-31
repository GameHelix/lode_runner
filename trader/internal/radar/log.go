package radar

import "sync"

// Log keeps the latest alerts in memory, numbered, so a stream can resume
// where it left off and a page can show the recent ones. It holds the
// last thousand. With a Store it keeps every alert beyond the engine's
// run: each is saved as it is added, a new run starts from the newest the
// store holds (Restore), and older alerts are read from the store.
type Log struct {
	Store Store
	// Logf, when set, reports alerts the store could not keep.
	Logf func(format string, args ...any)

	mu      sync.Mutex
	seq     int64
	entries []Entry
	subs    map[chan struct{}]bool
}

// Store keeps alerts beyond the engine's run.
type Store interface {
	SaveAlert(Entry) error
	// AlertsAfter returns up to limit alerts numbered above seq, oldest
	// first.
	AlertsAfter(seq int64, limit int) ([]Entry, error)
}

// Entry is an alert with its number in the log.
type Entry struct {
	Seq int64
	Alert
}

const keep = 1000

// Restore starts the log from alerts kept before, oldest first: it holds
// the newest of them, and numbering goes on after the last.
func (l *Log) Restore(entries []Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(entries) > keep {
		entries = entries[len(entries)-keep:]
	}
	l.entries = append([]Entry(nil), entries...)
	if n := len(entries); n > 0 && entries[n-1].Seq > l.seq {
		l.seq = entries[n-1].Seq
	}
}

// Add numbers an alert, keeps it, wakes the streams and saves it to the
// store.
func (l *Log) Add(a Alert) Entry {
	l.mu.Lock()
	l.seq++
	e := Entry{Seq: l.seq, Alert: a}
	l.entries = append(l.entries, e)
	if len(l.entries) > keep {
		l.entries = l.entries[len(l.entries)-keep:]
	}
	for ch := range l.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	l.mu.Unlock()
	if l.Store != nil {
		if err := l.Store.SaveAlert(e); err != nil && l.Logf != nil {
			l.Logf("radar: alert %d not saved: %v", e.Seq, err)
		}
	}
	return e
}

// After returns up to limit entries numbered above seq, oldest first.
// Entries older than those in memory come from the store.
func (l *Log) After(seq int64, limit int) []Entry {
	l.mu.Lock()
	older := l.Store != nil && len(l.entries) > 0 && seq < l.entries[0].Seq-1
	l.mu.Unlock()
	if older {
		out, err := l.Store.AlertsAfter(seq, limit)
		if err == nil {
			return out
		}
		if l.Logf != nil {
			l.Logf("radar: alerts after %d: %v", seq, err)
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	i := 0
	for i < len(l.entries) && l.entries[i].Seq <= seq {
		i++
	}
	out := l.entries[i:]
	if len(out) > limit {
		out = out[:limit]
	}
	return append([]Entry(nil), out...)
}

// Last returns the newest n entries, oldest first.
func (l *Log) Last(n int) []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.entries
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return append([]Entry(nil), out...)
}

// Subscribe returns a channel that gets a value when an alert is added,
// and a function to stop.
func (l *Log) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.subs == nil {
		l.subs = map[chan struct{}]bool{}
	}
	l.subs[ch] = true
	return ch, func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.subs, ch)
	}
}
