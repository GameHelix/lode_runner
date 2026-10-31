// Package engine calls the Candylario engine API: the radar's alerts as a
// stream, and a tenant's wallet and trades. Its contract is the engine's
// openapi.yaml.
package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// Error is a request the engine refused, such as refused.
type Error struct {
	Status  int
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return fmt.Sprintf("engine %d %s: %s", e.Status, e.Code, e.Message) }

// IsCode reports whether err is an engine error with that code.
func IsCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

func (c *Client) request(ctx context.Context, method, path string, body any) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		var wrap struct {
			Error *Error `json:"error"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if json.Unmarshal(raw, &wrap) != nil || wrap.Error == nil {
			return nil, &Error{Status: resp.StatusCode, Code: "internal", Message: http.StatusText(resp.StatusCode)}
		}
		wrap.Error.Status = resp.StatusCode
		return nil, wrap.Error
	}
	return resp, nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	resp, err := c.request(ctx, method, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}

// RadarAlert is one unusual move the engine's radar raised. Text is the
// alert as a chat shows it; Suggestion, when present, is the trade its
// record argues for.
type RadarAlert struct {
	Seq        int64       `json:"seq"`
	At         time.Time   `json:"at"`
	Coin       string      `json:"coin"`
	Change15m  float64     `json:"change_15m"`
	Change1h   float64     `json:"change_1h"`
	Text       string      `json:"text"`
	Suggestion *Suggestion `json:"suggestion"`
}

type Suggestion struct {
	Side        string  `json:"side"`
	StopLossPct float64 `json:"stop_loss_pct"`
}

// RadarStream passes the radar's alerts numbered above after, as they are
// raised, until the stream drops.
func (c *Client) RadarStream(ctx context.Context, after int64, fn func(RadarAlert) error) error {
	return c.stream(ctx, "/v1/radar/stream?after="+strconv.FormatInt(after, 10), func(data []byte) error {
		var a RadarAlert
		if err := json.Unmarshal(data, &a); err != nil {
			return fmt.Errorf("radar stream: %w", err)
		}
		return fn(a)
	})
}

// RadarSeq is the number of the radar's latest alert, 0 when it has none.
func (c *Client) RadarSeq(ctx context.Context) (int64, error) {
	var out struct {
		Alerts []RadarAlert `json:"alerts"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/radar/alerts?limit=1", nil, &out); err != nil {
		return 0, err
	}
	if len(out.Alerts) == 0 {
		return 0, nil
	}
	return out.Alerts[0].Seq, nil
}

// Notice is something an account's chats are told about its trading.
type Notice struct {
	Seq      int64     `json:"seq"`
	TenantID string    `json:"tenant_id"`
	At       time.Time `json:"at"`
	Text     string    `json:"text"`
}

// NoticeStream passes every account's notices numbered above after, as
// the engine records them, until the stream drops.
func (c *Client) NoticeStream(ctx context.Context, after int64, fn func(Notice) error) error {
	return c.stream(ctx, "/v1/notices/stream?after="+strconv.FormatInt(after, 10), func(data []byte) error {
		var n Notice
		if err := json.Unmarshal(data, &n); err != nil {
			return fmt.Errorf("notice stream: %w", err)
		}
		return fn(n)
	})
}

// NoticeSeq is the number of the newest notice, 0 when there is none.
func (c *Client) NoticeSeq(ctx context.Context) (int64, error) {
	var out struct {
		Notices []Notice `json:"notices"`
	}
	if err := c.do(ctx, http.MethodGet, "/v1/notices", nil, &out); err != nil {
		return 0, err
	}
	if len(out.Notices) == 0 {
		return 0, nil
	}
	return out.Notices[0].Seq, nil
}

func tenantPath(tenant, rest string) string {
	return "/v1/tenants/" + url.PathEscape(tenant) + rest
}

// Trade is a position the engine opened, with its stop-loss on the
// exchange. Mode is testnet or mainnet.
type Trade struct {
	Mode     string  `json:"mode"`
	Coin     string  `json:"coin"`
	Side     string  `json:"side"`
	Size     float64 `json:"size"`
	EntryPx  float64 `json:"entry_px"`
	USD      float64 `json:"usd"`
	Leverage int     `json:"leverage"`
	StopPx   float64 `json:"stop_px"`
}

// OpenTrade opens a position from the tenant's wallet: a market order of
// usd with its stop-loss, stop a fraction of the entry price.
func (c *Client) OpenTrade(ctx context.Context, tenant, coin, side string, usd float64, leverage int, stop float64) (Trade, error) {
	body := map[string]any{"coin": coin, "side": side, "usd": usd, "leverage": leverage, "stop_loss_pct": stop}
	var out Trade
	return out, c.do(ctx, http.MethodPost, tenantPath(tenant, "/trades"), body, &out)
}

// Closed is what a close did; Remaining is what did not fill.
type Closed struct {
	Mode           string  `json:"mode"`
	Coin           string  `json:"coin"`
	Size           float64 `json:"size"`
	AvgPx          float64 `json:"avg_px"`
	Remaining      float64 `json:"remaining"`
	StopsCancelled int     `json:"stops_cancelled"`
}

// CloseTrade closes the tenant's position in a coin and cancels its stops.
func (c *Client) CloseTrade(ctx context.Context, tenant, coin string) (Closed, error) {
	var out Closed
	return out, c.do(ctx, http.MethodPost, tenantPath(tenant, "/trades/close"), map[string]string{"coin": coin}, &out)
}

// Wallet is the tenant's agent wallet and, once approved, its account.
type Wallet struct {
	AgentApproved bool     `json:"agent_approved"`
	Mode          string   `json:"mode"`
	Account       *Account `json:"account"`
}

type Account struct {
	Value        float64    `json:"value"`
	Withdrawable float64    `json:"withdrawable"`
	Positions    []Position `json:"positions"`
}

// Position is an open position; Size is negative when short.
type Position struct {
	Coin          string  `json:"coin"`
	Size          float64 `json:"size"`
	EntryPx       float64 `json:"entry_px"`
	UnrealizedPnl float64 `json:"unrealized_pnl"`
	LiquidationPx float64 `json:"liquidation_px"`
	Leverage      int     `json:"leverage"`
}

func (c *Client) Wallet(ctx context.Context, tenant string) (Wallet, error) {
	var out Wallet
	return out, c.do(ctx, http.MethodGet, tenantPath(tenant, "/wallet"), nil, &out)
}

// stream reads server-sent events from path and gives fn each data field.
func (c *Client) stream(ctx context.Context, path string, fn func(data []byte) error) error {
	resp, err := c.request(ctx, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 4<<20)
	var data []byte
	for sc.Scan() {
		line := sc.Bytes()
		switch {
		case len(line) == 0 && len(data) > 0:
			if err := fn(data); err != nil {
				return err
			}
			data = data[:0]
		case bytes.HasPrefix(line, []byte("data:")):
			data = append(data, bytes.TrimSpace(line[5:])...)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	return errors.New("event stream closed")
}
