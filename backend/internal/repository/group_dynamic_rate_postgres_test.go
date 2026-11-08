//go:build unit

package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupDynamicRatePostgres(t *testing.T) {
	dsn := os.Getenv("LINAI_DYNAMIC_RATE_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_DYNAMIC_RATE_TEST_DSN for isolated PostgreSQL")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runGroupDynamicRateContract(t, db)
}
