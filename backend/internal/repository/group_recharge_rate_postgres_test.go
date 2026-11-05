//go:build unit

package repository

import (
	"database/sql"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupRechargeRatePostgresContract(t *testing.T) {
	dsn := os.Getenv("LINAI_RECHARGE_TEST_DSN")
	if dsn == "" {
		t.Skip("set LINAI_RECHARGE_TEST_DSN for the isolated PostgreSQL contract")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	runGroupRechargeRateContract(t, db)
}
