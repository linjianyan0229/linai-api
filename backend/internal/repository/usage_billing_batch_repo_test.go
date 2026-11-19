//go:build unit

package repository

import (
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUsageBillingBatchValidation(t *testing.T) {
	valid := func(id string) *service.UsageBillingCommand {
		return &service.UsageBillingCommand{RequestID: id, UserID: 7, APIKeyID: 3, BalanceCost: 0.000078125}
	}
	for _, tc := range []struct {
		name string
		cmds func() []*service.UsageBillingCommand
		want error
	}{
		{"nil command", func() []*service.UsageBillingCommand { return []*service.UsageBillingCommand{nil} }, errUsageBillingBatchNilCommand},
		{"missing request", func() []*service.UsageBillingCommand { return []*service.UsageBillingCommand{valid(" ")} }, service.ErrUsageBillingRequestIDRequired},
		{"duplicate request", func() []*service.UsageBillingCommand { return []*service.UsageBillingCommand{valid("a"), valid("a")} }, errUsageBillingBatchDuplicateRequest},
		{"different users", func() []*service.UsageBillingCommand {
			a, b := valid("a"), valid("b")
			b.UserID++
			return []*service.UsageBillingCommand{a, b}
		}, errUsageBillingBatchMixedUsers},
		{"non-finite cost", func() []*service.UsageBillingCommand {
			a := valid("a")
			a.AccountQuotaCost = math.Inf(1)
			return []*service.UsageBillingCommand{a}
		}, errUsageBillingBatchNonFiniteAmount},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateUsageBillingBatch(tc.cmds())
			require.ErrorIs(t, err, tc.want)
		})
	}
	cmd := valid(" request ")
	cmd.Normalize()
	fingerprint := cmd.RequestFingerprint
	userID, err := validateUsageBillingBatch([]*service.UsageBillingCommand{cmd})
	require.NoError(t, err)
	require.Equal(t, int64(7), userID)
	require.Equal(t, fingerprint, cmd.RequestFingerprint, "batch validation must preserve the command's original idempotency fingerprint")
	require.Equal(t, 0.00007813, cmd.BalanceCost)
}

func TestUsageBillingBatchAmountGroupingPreservesPrecisionAndOrder(t *testing.T) {
	cmds := []*service.UsageBillingCommand{
		{APIKeyID: 8, APIKeyRateLimitCost: 1000000000.1234567},
		{APIKeyID: 3, APIKeyRateLimitCost: 1},
		{APIKeyID: 8, APIKeyRateLimitCost: 0.00000001},
		{APIKeyID: 8, APIKeyRateLimitCost: 2},
	}
	groups, ids := groupUsageBillingAmounts(cmds, []bool{true, true, true, false}, func(c *service.UsageBillingCommand) (int64, float64, bool) {
		return c.APIKeyID, c.APIKeyRateLimitCost, true
	})
	require.Equal(t, []int64{3, 8}, ids, "database row locks must use ascending identifiers")
	require.Equal(t, []int{0, 2}, groups[8].indexes)
	require.Equal(t, "1000000000.12345671", groups[8].sum.StringFixed(8))
}
