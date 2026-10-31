// Package store keeps the notifier's data in its own Postgres database.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

var (
	ErrNotLinked = errors.New("this chat is not linked")
	ErrBadCode   = errors.New("the link expired or was used")
)

type Store struct{ pool *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.New("invalid DATABASE_URL")
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

// Migrate applies the embedded migrations in order, once each.
func Migrate(ctx context.Context, url string) error {
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "select pg_advisory_lock(hashtext('notifier-migrations'))"); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, "create table if not exists schema_migrations (version int primary key, applied_at timestamptz not null default now())"); err != nil {
		return err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	for _, name := range names {
		version, err := strconv.Atoi(strings.SplitN(strings.TrimPrefix(name, "migrations/"), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: bad name", name)
		}
		sql, err := migrations.ReadFile(name)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, "insert into schema_migrations (version) values ($1) on conflict do nothing", version)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			_, err = tx.Exec(ctx, string(sql))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func hash(code string) []byte {
	h := sha256.Sum256([]byte(code))
	return h[:]
}

// NewCode returns a one-time code that links a chat to the tenant until it
// expires.
func (s *Store) NewCode(ctx context.Context, tenant uuid.UUID, ttl time.Duration) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	code := base64.RawURLEncoding.EncodeToString(b)
	_, err := s.pool.Exec(ctx, "insert into link_codes (code_hash, tenant_id, expires_at) values ($1, $2, now() + $3::interval)",
		hash(code), tenant, fmt.Sprintf("%d seconds", int(ttl.Seconds())))
	return code, err
}

// Link is a chat that follows a tenant's agents.
type Link struct {
	Chat     int64
	Tenant   uuid.UUID
	Name     string
	LinkedAt time.Time
}

// Redeem uses a code to link the chat to the code's tenant, in place of
// any tenant it followed before.
func (s *Store) Redeem(ctx context.Context, code string, chat int64, name string) (Link, error) {
	l := Link{Chat: chat, Name: name}
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `update link_codes set used_at = now()
			where code_hash = $1 and used_at is null and expires_at > now() returning tenant_id`, hash(code)).Scan(&l.Tenant)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBadCode
		}
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `insert into links (chat_id, tenant_id, name) values ($1, $2, $3)
			on conflict (chat_id) do update set tenant_id = excluded.tenant_id, name = excluded.name, linked_at = now()
			returning linked_at`, chat, l.Tenant, name).Scan(&l.LinkedAt)
	})
	return l, err
}

func (s *Store) Link(ctx context.Context, chat int64) (Link, error) {
	l := Link{Chat: chat}
	err := s.pool.QueryRow(ctx, "select tenant_id, name, linked_at from links where chat_id = $1", chat).Scan(&l.Tenant, &l.Name, &l.LinkedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNotLinked
	}
	return l, err
}

func (s *Store) Links(ctx context.Context) ([]Link, error) {
	rows, err := s.pool.Query(ctx, "select chat_id, tenant_id, name, linked_at from links order by chat_id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Link, error) {
		var l Link
		return l, r.Scan(&l.Chat, &l.Tenant, &l.Name, &l.LinkedAt)
	})
}

// LinksOf lists the chats that follow a tenant.
func (s *Store) LinksOf(ctx context.Context, tenant uuid.UUID) ([]Link, error) {
	rows, err := s.pool.Query(ctx, "select chat_id, tenant_id, name, linked_at from links where tenant_id = $1 order by linked_at, chat_id", tenant)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Link, error) {
		var l Link
		return l, r.Scan(&l.Chat, &l.Tenant, &l.Name, &l.LinkedAt)
	})
}

func (s *Store) Unlink(ctx context.Context, chat int64) error {
	_, err := s.pool.Exec(ctx, "delete from links where chat_id = $1", chat)
	return err
}

// UnlinkOf disconnects a chat from a tenant, and reports whether it was
// connected to that tenant.
func (s *Store) UnlinkOf(ctx context.Context, tenant uuid.UUID, chat int64) (bool, error) {
	tag, err := s.pool.Exec(ctx, "delete from links where tenant_id = $1 and chat_id = $2", tenant, chat)
	return tag.RowsAffected() > 0, err
}

func (s *Store) Offset(ctx context.Context, bot string) (int64, error) {
	var next int64
	err := s.pool.QueryRow(ctx, "select next_update from offsets where bot = $1", bot).Scan(&next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return next, err
}

func (s *Store) SetOffset(ctx context.Context, bot string, next int64) error {
	_, err := s.pool.Exec(ctx, `insert into offsets (bot, next_update) values ($1, $2)
		on conflict (bot) do update set next_update = excluded.next_update`, bot, next)
	return err
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }
