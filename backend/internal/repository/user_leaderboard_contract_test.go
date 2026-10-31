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

	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUserLeaderboardPostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_LEADERBOARD_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_LEADERBOARD_TEST_DSN for isolated ranking tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()
	schema := fmt.Sprintf("leaderboard_%d", time.Now().UnixNano())
	_, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	defer func() {
		_, err := admin.ExecContext(context.Background(), "DROP SCHEMA "+pq.QuoteIdentifier(schema)+" CASCADE")
		require.NoError(t, err)
	}()
	connection, err := url.Parse(dsn)
	require.NoError(t, err)
	q := connection.Query()
	q.Set("search_path", schema)
	connection.RawQuery = q.Encode()
	db, err := sql.Open("postgres", connection.String())
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.ExecContext(ctx, `CREATE TABLE users(id BIGINT PRIMARY KEY,username TEXT,status TEXT DEFAULT 'active',deleted_at TIMESTAMPTZ);
        CREATE TABLE usage_logs(user_id BIGINT,model TEXT,requested_model TEXT,input_tokens INT DEFAULT 0,output_tokens INT DEFAULT 0,cache_creation_tokens INT DEFAULT 0,cache_read_tokens INT DEFAULT 0,actual_cost NUMERIC(20,8) DEFAULT 0);
        INSERT INTO users(id,username) SELECT i,'User '||i FROM generate_series(1,25) i;
        INSERT INTO usage_logs(user_id,model,input_tokens,actual_cost) SELECT i,'model-a',i*100,i::numeric/10 FROM generate_series(1,25) i;
        INSERT INTO users(id,username,status,deleted_at) VALUES(99,'Disabled','inactive',NULL),(100,'Deleted','active',NOW()),(101,'Empty','active',NULL);
        INSERT INTO usage_logs(user_id,model,input_tokens,actual_cost) VALUES(99,'private',100000,50),(100,'private',100000,50),(101,'failed-only',0,0);
        INSERT INTO usage_logs(user_id,model,requested_model,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens,actual_cost) VALUES(1,'private-upstream','model-b',1000,200,300,400,0.00000001),(2,'model-b',NULL,500,0,0,0,0),(3,'private-upstream',' model-b ',100,0,0,0,1);`)
	require.NoError(t, err)
	repo := NewUserLeaderboardRepository(db)
	items, err := repo.Top(ctx, "")
	require.NoError(t, err)
	require.Len(t, items, 20)
	require.Equal(t, "User 25", items[0].Username)
	filtered, err := repo.Top(ctx, "model-b")
	require.NoError(t, err)
	require.Len(t, filtered, 3)
	require.Equal(t, "User 1", filtered[0].Username)
	require.Equal(t, int64(1900), filtered[0].TotalTokens)
	require.Equal(t, "0.00000001", filtered[0].TotalQuota)
	require.Equal(t, "User 2", filtered[1].Username, "free calls with tokens are valid usage")
	models, err := repo.Models(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b"}, models)
	empty, err := repo.Top(ctx, "model-b' OR 1=1 --")
	require.NoError(t, err)
	require.Empty(t, empty, "model names are parameterized exact matches")
	_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(user_id,model,input_tokens,output_tokens,actual_cost) VALUES(4,'overflow',2000000000,2000000000,2),(5,'overflow',2000000000,2000000000,3),(6,'overflow',2000000000,2000000000,3)`)
	require.NoError(t, err)
	tied, err := repo.Top(ctx, "overflow")
	require.NoError(t, err)
	require.Len(t, tied, 3)
	require.Equal(t, int64(4000000000), tied[0].TotalTokens)
	require.Equal(t, "User 5", tied[0].Username)
	require.Equal(t, "User 6", tied[1].Username)
	require.Equal(t, "User 4", tied[2].Username, "ties use actual cost then stable user ID")
}
