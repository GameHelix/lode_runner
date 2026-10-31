// Command notifier runs the Candylario bot: it posts the engine's radar
// alerts to Telegram and connects chats to accounts.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/candylario/candylario/notifier/internal/api"
	"github.com/candylario/candylario/notifier/internal/engine"
	"github.com/candylario/candylario/notifier/internal/notifier"
	"github.com/candylario/candylario/notifier/internal/store"
	"github.com/candylario/candylario/notifier/internal/telegram"
)

const usage = `notifier — Candylario agents' messages on Telegram, and commands back

  notifier serve                  run the bot and the API for the app
  notifier link --tenant ID       print a one-time link that connects a
                                  Telegram chat to the tenant (10 minutes)

Needs TELEGRAM_BOT_TOKEN, ENGINE_API_URL, ENGINE_NOTIFIER_TOKEN (the engine's token for the notifier), DATABASE_URL and
NOTIFIER_API_TOKEN. The API listens on NOTIFIER_API_ADDR, :8080 by default.`

// commit is the source commit this binary was built from, set by the
// Dockerfile with -ldflags "-X main.commit=...".
var commit = "dev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(out, usage)
		return nil
	}
	var missing []string
	env := map[string]string{}
	for _, k := range []string{"TELEGRAM_BOT_TOKEN", "ENGINE_API_URL", "ENGINE_NOTIFIER_TOKEN", "DATABASE_URL", "NOTIFIER_API_TOKEN"} {
		if env[k] = os.Getenv(k); env[k] == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("set %s", strings.Join(missing, ", "))
	}
	if err := store.Migrate(ctx, env["DATABASE_URL"]); err != nil {
		return err
	}
	st, err := store.Open(ctx, env["DATABASE_URL"])
	if err != nil {
		return err
	}
	defer st.Close()
	bot := &telegram.Bot{HTTP: &http.Client{Timeout: 90 * time.Second}, BaseURL: os.Getenv("TELEGRAM_API_URL"), Token: env["TELEGRAM_BOT_TOKEN"]}
	logf := func(format string, args ...any) {
		fmt.Fprintf(out, "%s %s\n", time.Now().UTC().Format("2006-01-02 15:04:05Z"), fmt.Sprintf(format, args...))
	}
	switch args[0] {
	case "serve":
		if len(args) > 1 {
			return errors.New("serve takes no arguments")
		}
		me, err := bot.Me(ctx)
		if err != nil {
			return fmt.Errorf("telegram bot: %w", err)
		}
		svc := &notifier.Service{Bot: bot, Store: st, Logf: logf,
			Engine: &engine.Client{BaseURL: env["ENGINE_API_URL"], Token: env["ENGINE_NOTIFIER_TOKEN"], HTTP: &http.Client{}}}
		if v := os.Getenv("RADAR_CHANNEL_ID"); v != "" {
			if svc.RadarChannel, err = strconv.ParseInt(v, 10, 64); err != nil {
				return errors.New("RADAR_CHANNEL_ID must be a chat ID")
			}
		}
		a := &api.Server{Token: env["NOTIFIER_API_TOKEN"], Bot: me.Username, Store: st, Commit: commit, Logf: logf}
		addr := os.Getenv("NOTIFIER_API_ADDR")
		if addr == "" {
			addr = ":8080"
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		errc := make(chan error, 2)
		go func() {
			err := a.Serve(ctx, addr)
			if err != nil {
				err = fmt.Errorf("api: %w", err)
			}
			errc <- err
		}()
		go func() { errc <- svc.Run(ctx) }()
		err = <-errc
		cancel()
		if err2 := <-errc; err == nil {
			err = err2
		}
		return err
	case "link":
		fs := flag.NewFlagSet("link", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		tenant := fs.String("tenant", "", "tenant ID")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		id, err := uuid.Parse(*tenant)
		if err != nil || fs.NArg() != 0 {
			return errors.New("usage: notifier link --tenant ID")
		}
		me, err := bot.Me(ctx)
		if err != nil {
			return fmt.Errorf("telegram bot: %w", err)
		}
		code, err := st.NewCode(ctx, id, 10*time.Minute)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "https://t.me/%s?start=%s\nOpen it in Telegram within 10 minutes; it works once.\n", me.Username, code)
		return nil
	}
	return fmt.Errorf("unknown command %q; run `notifier help`", args[0])
}
