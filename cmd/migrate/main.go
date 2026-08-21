// cmd/migrate applies the numbered SQL migrations in migrations/.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/maczeo11/cinefund/internal/platform/config"
	"github.com/maczeo11/cinefund/migrations"
)

type migration struct {
	version int
	name    string
	up      string
	down    string
}

func main() {
	cmd := "up"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	cfg := config.MustLoad()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	conn, err := pgx.Connect(ctx, cfg.Postgres.DSN)
	if err != nil {
		fatal("connect: %v", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if err := ensureTable(ctx, conn); err != nil {
		fatal("create schema_migrations: %v", err)
	}

	all, err := loadMigrations()
	if err != nil {
		fatal("load migrations: %v", err)
	}

	switch cmd {
	case "up":
		err = up(ctx, conn, all)
	case "down":
		err = down(ctx, conn, all)
	case "status":
		err = status(ctx, conn, all)
	default:
		fatal("unknown command %q (want up|down|status)", cmd)
	}
	if err != nil {
		fatal("%s: %v", cmd, err)
	}
}

func ensureTable(ctx context.Context, conn *pgx.Conn) error {
	_, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     INTEGER PRIMARY KEY,
			name        TEXT NOT NULL,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	return err
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}

	byVersion := map[int]*migration{}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		// 0007_pledges.up.sql -> version 7, name "pledges"
		parts := strings.SplitN(name, "_", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("malformed migration filename %q", name)
		}
		version, err := strconv.Atoi(parts[0])
		if err != nil {
			return nil, fmt.Errorf("malformed version in %q: %w", name, err)
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, err
		}

		m := byVersion[version]
		if m == nil {
			m = &migration{version: version, name: strings.SplitN(parts[1], ".", 2)[0]}
			byVersion[version] = m
		}
		switch {
		case strings.HasSuffix(name, ".up.sql"):
			m.up = string(body)
		case strings.HasSuffix(name, ".down.sql"):
			m.down = string(body)
		default:
			return nil, fmt.Errorf("migration %q is neither .up.sql nor .down.sql", name)
		}
	}

	out := make([]migration, 0, len(byVersion))
	for _, m := range byVersion {
		if m.up == "" {
			return nil, fmt.Errorf("migration %04d_%s has no .up.sql", m.version, m.name)
		}
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func applied(ctx context.Context, conn *pgx.Conn) (map[int]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		seen[v] = true
	}
	return seen, rows.Err()
}

// up applies each pending migration in its own transaction, so a failure leaves
// every earlier migration committed and the failing one fully rolled back.
func up(ctx context.Context, conn *pgx.Conn, all []migration) error {
	seen, err := applied(ctx, conn)
	if err != nil {
		return err
	}

	n := 0
	for _, m := range all {
		if seen[m.version] {
			continue
		}
		if err := inTx(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.up); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
				m.version, m.name)
			return err
		}); err != nil {
			return fmt.Errorf("%04d_%s: %w", m.version, m.name, err)
		}
		fmt.Printf("applied  %04d_%s\n", m.version, m.name)
		n++
	}
	if n == 0 {
		fmt.Println("nothing to apply")
	}
	return nil
}

func down(ctx context.Context, conn *pgx.Conn, all []migration) error {
	var latest int
	err := conn.QueryRow(ctx,
		`SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1`).Scan(&latest)
	if errors.Is(err, pgx.ErrNoRows) {
		fmt.Println("nothing to roll back")
		return nil
	}
	if err != nil {
		return err
	}

	for _, m := range all {
		if m.version != latest {
			continue
		}
		if m.down == "" {
			return fmt.Errorf("%04d_%s has no .down.sql", m.version, m.name)
		}
		if err := inTx(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.down); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, m.version)
			return err
		}); err != nil {
			return fmt.Errorf("%04d_%s: %w", m.version, m.name, err)
		}
		fmt.Printf("reverted %04d_%s\n", m.version, m.name)
		return nil
	}
	return fmt.Errorf("migration %d is recorded as applied but its files are missing", latest)
}

func status(ctx context.Context, conn *pgx.Conn, all []migration) error {
	seen, err := applied(ctx, conn)
	if err != nil {
		return err
	}
	for _, m := range all {
		mark := "pending"
		if seen[m.version] {
			mark = "applied"
		}
		fmt.Printf("%-8s %04d_%s\n", mark, m.version, m.name)
	}
	return nil
}

func inTx(ctx context.Context, conn *pgx.Conn, fn func(pgx.Tx) error) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "migrate: "+format+"\n", args...)
	os.Exit(1)
}
