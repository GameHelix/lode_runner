// Package engine calls the Candylario engine API: the radar's alerts as a
// stream. Its contract is the engine's openapi.yaml.
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
// alert as a chat shows it.
type RadarAlert struct {
	Seq       int64     `json:"seq"`
	At        time.Time `json:"at"`
	Coin      string    `json:"coin"`
	Change15m float64   `json:"change_15m"`
	Change1h  float64   `json:"change_1h"`
	Text      string    `json:"text"`
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
