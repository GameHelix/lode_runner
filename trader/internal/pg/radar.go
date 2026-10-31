package pg

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// RadarAlert is one of the radar's alerts as stored: its number, when and
// on which coin it was raised, the rule that raised it, and the whole alert
// as JSON.
type RadarAlert struct {
	Seq   int64
	At    time.Time
	Coin  string
	Rule  string
	Alert json.RawMessage
}

func (s *Store) AddRadarAlert(ctx context.Context, a RadarAlert) error {
	_, err := s.pool.Exec(ctx, "insert into radar_alerts (seq, at, coin, rule, alert) values ($1, $2, $3, $4, $5)", a.Seq, a.At, a.Coin, a.Rule, string(a.Alert))
	return unique(err)
}

// RadarAlertsAfter returns up to limit alerts numbered above seq, oldest
// first.
func (s *Store) RadarAlertsAfter(ctx context.Context, seq int64, limit int) ([]RadarAlert, error) {
	return s.radarAlerts(ctx, "select seq, at, coin, rule, alert::text from radar_alerts where seq > $1 order by seq limit $2", seq, limit)
}

// LastRadarAlerts returns the newest n alerts, oldest first.
func (s *Store) LastRadarAlerts(ctx context.Context, n int) ([]RadarAlert, error) {
	return s.radarAlerts(ctx, "select * from (select seq, at, coin, rule, alert::text from radar_alerts order by seq desc limit $1) newest order by seq", n)
}

func (s *Store) radarAlerts(ctx context.Context, query string, args ...any) ([]RadarAlert, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (RadarAlert, error) {
		var a RadarAlert
		var raw string
		err := r.Scan(&a.Seq, &a.At, &a.Coin, &a.Rule, &raw)
		a.At, a.Alert = a.At.UTC(), json.RawMessage(raw)
		return a, err
	})
}
