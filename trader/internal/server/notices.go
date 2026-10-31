package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/jobloom/candylario/trader/api"
	"github.com/jobloom/candylario/trader/internal/pg"
)

func limitsOut(l pg.Limits) api.Limits {
	return api.Limits{MaxUSD: l.MaxUSD, MaxLeverage: l.MaxLeverage, DailyLossUSD: l.DailyLossUSD}
}

func (s *Server) limitsReply(r *http.Request, tenant uuid.UUID) (int, any, error) {
	l, caps, err := s.Fleet.Limits(r.Context(), tenant)
	if err != nil {
		return 0, nil, err
	}
	pnl, err := s.Fleet.TodayPnl(r.Context(), tenant)
	if err != nil {
		return 0, nil, err
	}
	return http.StatusOK, api.AccountLimits{Limits: limitsOut(l), Caps: limitsOut(caps), TodayPnl: pnl}, nil
}

func (s *Server) getLimits(w http.ResponseWriter, r *http.Request) (int, any, error) {
	ids, err := pathIDs(r, "tenant")
	if err != nil {
		return 0, nil, err
	}
	if _, err := s.Fleet.Store.Tenant(r.Context(), ids[0]); err != nil {
		return 0, nil, err
	}
	return s.limitsReply(r, ids[0])
}

func (s *Server) setLimits(w http.ResponseWriter, r *http.Request) (int, any, error) {
	ids, err := pathIDs(r, "tenant")
	if err != nil {
		return 0, nil, err
	}
	var in api.Limits
	if err := decode(w, r, &in); err != nil {
		return 0, nil, err
	}
	if err := s.Fleet.SetLimits(r.Context(), ids[0], pg.Limits{MaxUSD: in.MaxUSD, MaxLeverage: in.MaxLeverage, DailyLossUSD: in.DailyLossUSD}); err != nil {
		return 0, nil, err
	}
	return s.limitsReply(r, ids[0])
}

func noticeOut(n pg.Notice) api.Notice {
	return api.Notice{Seq: n.Seq, TenantID: n.Tenant.String(), At: n.At, Text: n.Text}
}

// notices lists every account's notices after a seq, or the latest one.
func (s *Server) notices(w http.ResponseWriter, r *http.Request) (int, any, error) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			return 0, nil, badRequest("limit must be 1–500")
		}
		limit = n
	}
	out := api.NoticeList{Notices: []api.Notice{}}
	if r.URL.Query().Get("after") == "" {
		last, err := s.Fleet.Store.LastNotice(r.Context())
		if err != nil || last == 0 {
			return http.StatusOK, out, err
		}
		ns, err := s.Fleet.Store.NoticesAfter(r.Context(), last-1, 1)
		for _, n := range ns {
			out.Notices = append(out.Notices, noticeOut(n))
		}
		return http.StatusOK, out, err
	}
	seq, err := after(r)
	if err != nil {
		return 0, nil, err
	}
	ns, err := s.Fleet.Store.NoticesAfter(r.Context(), seq, limit)
	for _, n := range ns {
		out.Notices = append(out.Notices, noticeOut(n))
	}
	return http.StatusOK, out, err
}

// noticeStream sends every account's notices as server-sent events as the
// engine records them.
func (s *Server) noticeStream(w http.ResponseWriter, r *http.Request) {
	seq, err := after(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	wake, done := s.Fleet.SubscribeNotices()
	defer done()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	rc.Flush()
	for {
		ns, err := s.Fleet.Store.NoticesAfter(r.Context(), seq, 500)
		if err != nil {
			if r.Context().Err() == nil {
				s.Logf("notice stream: %v", err)
			}
			return
		}
		for _, n := range ns {
			raw, _ := json.Marshal(noticeOut(n))
			fmt.Fprintf(w, "id: %d\nevent: notice\ndata: %s\n\n", n.Seq, raw)
			seq = n.Seq
		}
		if len(ns) > 0 {
			if rc.Flush() != nil {
				return
			}
			continue
		}
		select {
		case <-r.Context().Done():
			return
		case <-wake:
		case <-time.After(15 * time.Second):
			io.WriteString(w, ": waiting\n\n")
			if rc.Flush() != nil {
				return
			}
		}
	}
}
