package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Client calls the engine API.
type Client struct {
	BaseURL string // for example http://127.0.0.1:8080
	Token   string
	HTTP    *http.Client
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) request(ctx context.Context, method, path string, in any) (*http.Request, error) {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	req, err := c.request(ctx, method, path, in)
	if err != nil {
		return err
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return readError(resp)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func readError(resp *http.Response) error {
	var body struct {
		Error *Error `json:"error"`
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if json.Unmarshal(raw, &body) != nil || body.Error == nil {
		return &Error{Status: resp.StatusCode, Code: "internal", Message: http.StatusText(resp.StatusCode)}
	}
	body.Error.Status = resp.StatusCode
	return body.Error
}

func tenantPath(tenant string, rest ...string) string {
	p := "/v1/tenants/" + url.PathEscape(tenant)
	for _, r := range rest {
		p += "/" + url.PathEscape(r)
	}
	return p
}

func (c *Client) CreateTenant(ctx context.Context, t NewTenant) (Tenant, error) {
	var out Tenant
	return out, c.do(ctx, http.MethodPost, "/v1/tenants", t, &out)
}

// CloseTenant closes an account for good: its wallet key is erased and its
// name is gone.
func (c *Client) CloseTenant(ctx context.Context, tenant string) (Tenant, error) {
	var out Tenant
	return out, c.do(ctx, http.MethodDelete, tenantPath(tenant), nil, &out)
}

// Wallet returns the tenant's agent wallet and, once approved, its account.
// A tenant without one gets a not_found error.
func (c *Client) Wallet(ctx context.Context, tenant string) (Wallet, error) {
	var out Wallet
	return out, c.do(ctx, http.MethodGet, tenantPath(tenant, "wallet"), nil, &out)
}

// WalletApprovals gives the typed data the account's own wallet signs, on
// the chain that wallet is on (0 for the default), making the agent wallet
// first if the tenant has none.
func (c *Client) WalletApprovals(ctx context.Context, tenant, user, mode string, chainID int64) (WalletApprovals, error) {
	q := url.Values{"user": {user}, "mode": {mode}}
	if chainID > 0 {
		q.Set("chain_id", strconv.FormatInt(chainID, 10))
	}
	var out WalletApprovals
	return out, c.do(ctx, http.MethodGet, tenantPath(tenant, "wallet", "approvals")+"?"+q.Encode(), nil, &out)
}

// ApproveWallet submits the approvals the account's wallet signed; the
// engine confirms them with the exchange.
func (c *Client) ApproveWallet(ctx context.Context, tenant string, a WalletApproval) (Wallet, error) {
	var out Wallet
	return out, c.do(ctx, http.MethodPost, tenantPath(tenant, "wallet", "approvals"), a, &out)
}

// Limits returns the account's limits, the engine's caps and today's P&L.
func (c *Client) Limits(ctx context.Context, tenant string) (AccountLimits, error) {
	var out AccountLimits
	return out, c.do(ctx, http.MethodGet, tenantPath(tenant, "limits"), nil, &out)
}

// SetLimits replaces the account's own limits, which stay within the caps.
func (c *Client) SetLimits(ctx context.Context, tenant string, l Limits) (AccountLimits, error) {
	var out AccountLimits
	return out, c.do(ctx, http.MethodPut, tenantPath(tenant, "limits"), l, &out)
}
