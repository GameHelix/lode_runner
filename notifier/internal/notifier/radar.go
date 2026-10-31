package notifier

import (
	"context"
	"time"

	"github.com/candylario/candylario/notifier/internal/engine"
)

// staleAfter is how old an alert may be and still go out: one that piled
// up while the notifier was away longer is skipped, not posted late.
const staleAfter = 10 * time.Minute

// radar passes the engine's radar alerts to the radar channel and to every
// connected chat, with trade buttons there, in order and once each. It goes
// on from the last alert it passed, across restarts on either side,
// reconnects when the stream drops, and starts over from the engine's
// latest alert when the engine's numbering is behind it. The cursor is
// kept under the channel's chat, or 0 without a channel.
func (s *Service) radar(ctx context.Context) {
	seq, err := s.radarStart(ctx)
	for err != nil && sleep(ctx, s.Retry) {
		s.Logf("radar: %v", err)
		seq, err = s.radarStart(ctx)
	}
	for ctx.Err() == nil {
		err := s.Engine.RadarStream(ctx, seq, func(a engine.RadarAlert) error {
			if time.Since(a.At) <= staleAfter {
				if err := s.pass(ctx, a); err != nil {
					return err
				}
			}
			seq = a.Seq
			// Saved even while shutting down, so a restart neither
			// repeats this alert nor skips the next.
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := s.Store.SetRadarCursor(sctx, s.RadarChannel, seq); err != nil {
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

// pass sends one alert to the channel and to every connected chat.
func (s *Service) pass(ctx context.Context, a engine.RadarAlert) error {
	if s.RadarChannel != 0 {
		if err := s.send(ctx, s.RadarChannel, a.Text); err != nil {
			return err
		}
	}
	links, err := s.Store.Links(ctx)
	if err != nil {
		s.Logf("database: %v", err)
		return nil
	}
	buttons := tradeButtons(a)
	for _, l := range links {
		if err := s.send(ctx, l.Chat, a.Text, buttons...); err != nil {
			return err
		}
	}
	return nil
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
		// Kept at once, so a restart before the first alert goes on from
		// here instead of from whatever is latest by then.
		return latest, s.Store.SetRadarCursor(ctx, s.RadarChannel, latest)
	}
	return cur, nil
}
