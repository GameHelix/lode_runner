package notifier

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/candylario/candylario/notifier/internal/engine"
)

const noticesCursor = "notices"

// notices passes the engine's notices about each account's trading, such
// as a stop that hit, to the chats connected to that account, in order and
// once each. It goes on from the last notice it passed, across restarts on
// either side; unlike an alert, a notice is never too old to tell.
func (s *Service) notices(ctx context.Context) {
	seq, err := s.noticesStart(ctx)
	for err != nil && sleep(ctx, s.Retry) {
		s.Logf("notices: %v", err)
		seq, err = s.noticesStart(ctx)
	}
	for ctx.Err() == nil {
		err := s.Engine.NoticeStream(ctx, seq, func(n engine.Notice) error {
			if tenant, err := uuid.Parse(n.TenantID); err == nil {
				links, err := s.Store.LinksOf(ctx, tenant)
				if err != nil {
					return err
				}
				for _, l := range links {
					if err := s.send(ctx, l.Chat, n.Text); err != nil {
						return err
					}
				}
			}
			seq = n.Seq
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if err := s.Store.SetStreamCursor(sctx, noticesCursor, seq); err != nil {
				s.Logf("database: %v", err)
			}
			return nil
		})
		if ctx.Err() != nil {
			return
		}
		s.Logf("notice stream: %v", err)
		sleep(ctx, s.Retry)
		if latest, err := s.Engine.NoticeSeq(ctx); err == nil && latest < seq {
			seq = latest
		}
	}
}

// noticesStart is where the notices go on from: the cursor, or the
// engine's latest notice the first time and after the engine started over.
func (s *Service) noticesStart(ctx context.Context) (int64, error) {
	latest, err := s.Engine.NoticeSeq(ctx)
	if err != nil {
		return 0, err
	}
	cur, ok, err := s.Store.StreamCursor(ctx, noticesCursor)
	if err != nil {
		return 0, err
	}
	if !ok || cur > latest {
		// Kept at once, so a restart before the first notice goes on from
		// here instead of from whatever is latest by then.
		return latest, s.Store.SetStreamCursor(ctx, noticesCursor, latest)
	}
	return cur, nil
}
