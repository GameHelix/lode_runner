package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/jobloom/candylario/notifier/internal/store"
)

// testDatabase creates an empty, migrated database on the server in
// TEST_DATABASE_URL, or skips the test.
func testDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("set TEST_DATABASE_URL to run the notifier tests")
	}
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close(ctx)
	name := "notifier_test_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	if _, err := admin.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, err := pgx.Connect(context.Background(), base); err == nil {
			c.Exec(context.Background(), "drop database "+name+" with (force)")
			c.Close(context.Background())
		}
	})
	i := strings.LastIndex(base, "/")
	url := base[:i+1] + name
	if q := strings.Index(base[i:], "?"); q >= 0 {
		url += base[i+q:]
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	return url
}

type client struct {
	t     *testing.T
	srv   *httptest.Server
	token string
}

func (c client) do(method, path string, token string) (int, map[string]any) {
	c.t.Helper()
	req, err := http.NewRequest(method, c.srv.URL+path, nil)
	if err != nil {
		c.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.srv.Client().Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		c.t.Fatalf("%s %s: body is not JSON: %v", method, path, err)
	}
	return resp.StatusCode, body
}

func (c client) call(method, path string) (int, map[string]any) {
	c.t.Helper()
	return c.do(method, path, c.token)
}

func errorCode(body map[string]any) string {
	e, _ := body["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestLinksAndChats(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, testDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := &Server{Token: "secret", Bot: "TestBot", Store: st}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	c := client{t, srv, "secret"}
	tenant := uuid.New()
	base := "/v1/tenants/" + tenant.String()

	if status, body := c.do(http.MethodGet, "/v1/health", ""); status != 200 || body["ok"] != true {
		t.Fatalf("health: %d %v", status, body)
	}
	for _, token := range []string{"", "wrong"} {
		if status, body := c.do(http.MethodPost, base+"/links", token); status != 401 || errorCode(body) != "unauthorized" {
			t.Fatalf("token %q: %d %v", token, status, body)
		}
	}
	if status, body := c.call(http.MethodPost, "/v1/tenants/nope/links"); status != 400 || errorCode(body) != "bad_request" {
		t.Fatalf("bad tenant: %d %v", status, body)
	}
	if status, body := c.call(http.MethodGet, "/v1/nothing"); status != 404 || errorCode(body) != "not_found" {
		t.Fatalf("unknown route: %d %v", status, body)
	}

	status, body := c.call(http.MethodPost, base+"/links")
	if status != 201 {
		t.Fatalf("link: %d %v", status, body)
	}
	url, _ := body["url"].(string)
	code, ok := strings.CutPrefix(url, "https://t.me/TestBot?start=")
	if !ok || code == "" {
		t.Fatalf("link url %q", url)
	}
	expires, err := time.Parse(time.RFC3339, body["expires_at"].(string))
	if err != nil || time.Until(expires) < 9*time.Minute || time.Until(expires) > 11*time.Minute {
		t.Fatalf("expires_at %v: %v", body["expires_at"], err)
	}

	if status, body := c.call(http.MethodGet, base+"/chats"); status != 200 || len(body["chats"].([]any)) != 0 {
		t.Fatalf("chats before linking: %d %v", status, body)
	}
	link, err := st.Redeem(ctx, code, 42, "@alice")
	if err != nil || link.Tenant != tenant {
		t.Fatalf("redeem: %v %v", link, err)
	}
	if _, err := st.Redeem(ctx, code, 43, "@bob"); err == nil {
		t.Fatal("a code works once")
	}
	status, body = c.call(http.MethodGet, base+"/chats")
	chats, _ := body["chats"].([]any)
	if status != 200 || len(chats) != 1 {
		t.Fatalf("chats: %d %v", status, body)
	}
	chat := chats[0].(map[string]any)
	if chat["chat_id"] != float64(42) || chat["name"] != "@alice" {
		t.Fatalf("chat %v", chat)
	}
	if _, err := time.Parse(time.RFC3339, chat["linked_at"].(string)); err != nil {
		t.Fatalf("linked_at: %v", err)
	}

	other := "/v1/tenants/" + uuid.NewString()
	if status, body := c.call(http.MethodDelete, other+"/chats/42"); status != 404 || errorCode(body) != "not_found" {
		t.Fatalf("another tenant's delete: %d %v", status, body)
	}
	if status, body := c.call(http.MethodDelete, base+"/chats/x"); status != 400 {
		t.Fatalf("bad chat: %d %v", status, body)
	}
	if status, body := c.call(http.MethodDelete, base+"/chats/42"); status != 200 || body["ok"] != true {
		t.Fatalf("delete: %d %v", status, body)
	}
	if status, body := c.call(http.MethodDelete, base+"/chats/42"); status != 404 {
		t.Fatalf("delete again: %d %v", status, body)
	}
	if status, body := c.call(http.MethodGet, base+"/chats"); status != 200 || len(body["chats"].([]any)) != 0 {
		t.Fatalf("chats after delete: %d %v", status, body)
	}
}

func TestServeStopsWithContext(t *testing.T) {
	st, err := store.Open(context.Background(), testDatabase(t))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- (&Server{Token: "t", Store: st}).Serve(ctx, "127.0.0.1:0") }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not stop")
	}
}
