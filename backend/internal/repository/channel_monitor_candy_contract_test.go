//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// A dedicated DSN enables the real PostgreSQL contract without the repository
// suite's Docker harness. All writes stay inside a unique, disposable schema.
func TestCandyRepositoryPostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_CANDY_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_CANDY_TEST_DSN for the PostgreSQL candy contract")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer admin.Close()
	schema := fmt.Sprintf("linai_candy_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, dropErr := admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, dropErr)
	}()
	connection, err := url.Parse(dsn)
	require.NoError(t, err)
	query := connection.Query()
	query.Set("search_path", schema)
	connection.RawQuery = query.Encode()
	db, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	defer db.Close()
	_, err = db.ExecContext(ctx, `CREATE TABLE channel_monitors (
        id BIGINT PRIMARY KEY, enabled BOOLEAN NOT NULL, provider TEXT NOT NULL,
        check_mode TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL, last_checked_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "246_channel_monitor_candy.sql"))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	_, err = db.ExecContext(ctx, `INSERT INTO channel_monitors(id,enabled,provider,check_mode,updated_at,candy_enabled)
        VALUES(1,true,'openai','probe',$1,true),(2,true,'anthropic','probe',$1,true)`, now)
	require.NoError(t, err)
	repo := NewChannelMonitorCandyRepository(db)
	monitor := &service.ChannelMonitor{ID: 1, UpdatedAt: now}

	var workers sync.WaitGroup
	winners := make(chan string, 20)
	failures := make(chan error, 20)
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			attempt := fmt.Sprintf("attempt-%d", i)
			claimed, claimErr := repo.Claim(ctx, monitor, "revision-a", attempt, now)
			if claimErr != nil {
				failures <- claimErr
			}
			if claimed {
				winners <- attempt
			}
		}(i)
	}
	workers.Wait()
	close(winners)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	require.Len(t, winners, 1, "only one process may submit a chargeable check")
	winner := <-winners
	point := service.MonitorCandyPoint{Status: "passed", CheckedAt: now}
	require.NoError(t, repo.Complete(ctx, 1, "revision-a", winner, point))
	require.NoError(t, repo.Complete(ctx, 1, "revision-a", winner, point))
	states, err := repo.ListStates(ctx, []int64{1})
	require.NoError(t, err)
	require.Len(t, states[1].History, 1, "ambiguous completion retries must not duplicate history")

	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	monitors := NewChannelMonitorRepository(client, db)
	require.NoError(t, monitors.MarkChecked(ctx, 1, now.Add(time.Minute)))
	var revision, checkedAt time.Time
	err = db.QueryRowContext(ctx, `SELECT updated_at,last_checked_at FROM channel_monitors WHERE id=1`).Scan(&revision, &checkedAt)
	require.NoError(t, err)
	require.True(t, now.Equal(revision), "normal probes must preserve the config revision and candy history")
	require.True(t, now.Add(time.Minute).Equal(checkedAt))
	require.ErrorIs(t, monitors.MarkChecked(ctx, 999, now), service.ErrChannelMonitorNotFound)

	claimed, err := repo.Claim(ctx, monitor, "revision-a", "too-soon", now.Add(9*time.Minute))
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = repo.Claim(ctx, monitor, "revision-a", "on-time", now.Add(10*time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.Complete(ctx, 1, "revision-a", "on-time", point))

	monitor.UpdatedAt = now.Add(time.Second)
	claimed, err = repo.Claim(ctx, monitor, "revision-b", "stale-config", now.Add(11*time.Minute))
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = db.ExecContext(ctx, `UPDATE channel_monitors SET updated_at=$1 WHERE id=1`, monitor.UpdatedAt)
	require.NoError(t, err)
	claimed, err = repo.Claim(ctx, monitor, "revision-b", "new-config", now.Add(11*time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	require.NoError(t, repo.Complete(ctx, 1, "revision-a", winner, point))
	states, err = repo.ListStates(ctx, []int64{1})
	require.NoError(t, err)
	require.Empty(t, states[1].History, "new model/rekey starts a fresh history and rejects stale worker results")
	require.NoError(t, repo.Complete(ctx, 1, "revision-b", "new-config", point))
	for i := 1; i <= 65; i++ {
		attempt := fmt.Sprintf("retention-%d", i)
		claimed, err = repo.Claim(ctx, monitor, "revision-b", attempt, now.Add(time.Duration(11+i*10)*time.Minute))
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, repo.Complete(ctx, 1, "revision-b", attempt, point))
	}
	states, err = repo.ListStates(ctx, []int64{1})
	require.NoError(t, err)
	require.Len(t, states[1].History, 60)
	other := &service.ChannelMonitor{ID: 2, UpdatedAt: now}
	claimed, err = repo.Claim(ctx, other, "revision-a", "not-openai", now)
	require.NoError(t, err)
	require.False(t, claimed)
	_, err = db.ExecContext(ctx, `UPDATE channel_monitors SET candy_enabled=false WHERE id=1`)
	require.NoError(t, err)
	claimed, err = repo.Claim(ctx, monitor, "revision-b", "disabled", now.Add(24*time.Hour))
	require.NoError(t, err)
	require.False(t, claimed)
}
