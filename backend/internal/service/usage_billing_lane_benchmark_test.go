//go:build unit

package service

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"testing"
)

type billingLaneBenchmarkRepo struct {
	UsageBillingBatchRepository
	transactions atomic.Uint64
}

func (r *billingLaneBenchmarkRepo) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	r.transactions.Add(1)
	return &UsageBillingApplyResult{Applied: true}, nil
}

func (r *billingLaneBenchmarkRepo) ApplyBatch(_ context.Context, cmds []*UsageBillingCommand) ([]UsageBillingBatchOutcome, error) {
	r.transactions.Add(1)
	out := make([]UsageBillingBatchOutcome, len(cmds))
	for i := range out {
		out[i].Result = &UsageBillingApplyResult{Applied: true}
	}
	return out, nil
}

// Measures lane overhead and command-to-transaction coalescing, not database latency.
func BenchmarkUsageBillingLaneCombiner(b *testing.B) {
	for _, users := range []uint64{1, 64} {
		b.Run(fmt.Sprintf("users=%d", users), func(b *testing.B) {
			repo := &billingLaneBenchmarkRepo{}
			lanes := NewUsageBillingLaneCombiner(repo)
			var requests atomic.Uint64
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					id := requests.Add(1)
					_, err := lanes.Apply(context.Background(), &UsageBillingCommand{
						RequestID: strconv.FormatUint(id, 10), UserID: int64(id%users + 1), APIKeyID: 1, BalanceCost: 0.00000001,
					})
					if err != nil {
						b.Error(err)
					}
				}
			})
			b.StopTimer()
			lanes.Stop()
			if tx := repo.transactions.Load(); tx > 0 {
				b.ReportMetric(float64(requests.Load())/float64(tx), "commands/tx")
			}
		})
	}
}
