package notifier

import (
	"context"

	"github.com/candylario/candylario/notifier/internal/engine"
)

// radar passes the engine's radar alerts to the radar channel. It starts
// from the latest alert, so a restart repeats nothing, reconnects when the
// stream drops, and starts over when the engine's numbering has reset.
func (s *Service) radar(ctx context.Context) {
	seq, err := s.Engine.RadarSeq(ctx)
	for err != nil && sleep(ctx, s.Retry) {
		s.Logf("radar: %v", err)
		seq, err = s.Engine.RadarSeq(ctx)
	}
	for ctx.Err() == nil {
		err := s.Engine.RadarStream(ctx, seq, func(a engine.RadarAlert) error {
			if err := s.send(ctx, s.RadarChannel, a.Text); err != nil {
				return err
			}
			seq = a.Seq
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
