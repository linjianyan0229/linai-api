package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyConcurrencyResponse(t *testing.T) {
	for _, limit := range []int{0, 3} {
		out := APIKeyFromService(&service.APIKey{Concurrency: limit, CurrentConcurrency: 2})
		require.Equal(t, limit, out.Concurrency)
		require.Equal(t, 2, out.CurrentConcurrency)
		payload, err := json.Marshal(out)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal(payload, &got))
		require.Equal(t, float64(limit), got["concurrency"])
	}
}
