package notifier

import (
	"context"
	"time"

	"github.com/candylario/candylario/notifier/internal/engine"
)

// staleAfter is how old an alert may be and still go out: one that piled
// up while the notifier was away longer is skipped, not posted late.
const staleAfter = 10 * time.Minute

// radar passes the engine's radar alerts to the radar channel, in order and
// once each. It goes on from the last alert the channel got, across
// restarts on either side, reconnects when the stream drops, and starts
// over from the engine's latest alert when the engine's numbering is behind
// it.
func (s *Service) radar(ctx context.Context) {
	seq, err := s.radarStart(ctx)
	for err != nil && sleep(ctx, s.Retry) {
		s.Logf("radar: %v", err)
		seq, err = s.radarStart(ctx)
	}
	for ctx.Err() == nil {
		err := s.Engine.RadarStream(ctx, seq, func(a engine.RadarAlert) error {
			if time.Since(a.At) <= staleAfter {
				if err := s.send(ctx, s.RadarChannel, a.Text); err != nil {
					return err
				}
			}
			seq = a.Seq
			if err := s.Store.SetRadarCursor(ctx, s.RadarChannel, seq); err != nil {
				s.Logf("database: %v", err)
			}
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		s.Logf("radar stream: %v", err)
		sleep(ctx, s.Retry)
		if latest, err := s.Engine.RadarSeq(ctx); err == nil && latest < seq {
			seq = latest
		}
	}
}

// radarStart is where the channel goes on from: its cursor, or the
// engine's latest alert the first time and after the engine started over.
func (s *Service) radarStart(ctx context.Context) (int64, error) {
	latest, err := s.Engine.RadarSeq(ctx)
	if err != nil {
		return 0, err
	}
	cur, ok, err := s.Store.RadarCursor(ctx, s.RadarChannel)
	if err != nil {
		return 0, err
	}
	if !ok || cur > latest {
		return latest, nil
	}
	return cur, nil
}
