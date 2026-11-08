//go:build unit

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementPinningPostgres(t *testing.T) {
	dsn := os.Getenv("LINAI_ANNOUNCEMENT_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_ANNOUNCEMENT_TEST_DSN for isolated pinning tests")
	}
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = admin.Close() })
	ctx := context.Background()
	schema := fmt.Sprintf("announcement_pin_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := admin.ExecContext(ctx, "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, err)
	})
	connection, err := url.Parse(dsn)
	require.NoError(t, err)
	query := connection.Query()
	query.Set("search_path", schema)
	connection.RawQuery = query.Encode()
	db, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, `CREATE TABLE users(id BIGSERIAL PRIMARY KEY,email TEXT,password_hash TEXT)`)
	require.NoError(t, err)
	for _, file := range []string{"045_add_announcements.sql", "068_add_announcement_notify_mode.sql", "251_announcement_email_push.sql", "255_announcement_pinning.sql"} {
		contents, err := os.ReadFile("../../migrations/" + file)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(contents))
		require.NoError(t, err, file)
	}
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	runAnnouncementPinningContract(t, db, client)
}
