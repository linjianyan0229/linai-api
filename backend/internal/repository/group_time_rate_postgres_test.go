//go:build unit

package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupTimeRatePostgres(t *testing.T) {
	dsn := os.Getenv("LINAI_TIME_RATE_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_TIME_RATE_TEST_DSN for the isolated PostgreSQL contract")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runGroupTimeRateContract(t, db)
}
