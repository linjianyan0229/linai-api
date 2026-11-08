package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupDynamicRateDTOPrivacy(t *testing.T) {
	min, max := .16, .2
	g := &service.Group{ID: 7, DynamicRateEnabled: true, DynamicRateFactor: 2, DynamicRateMin: &min, DynamicRateMax: &max}
	public, err := json.Marshal(GroupFromService(g))
	require.NoError(t, err)
	require.Contains(t, string(public), `"dynamic_rate_enabled":true`)
	require.Contains(t, string(public), `"dynamic_rate_min":0.16`)
	require.NotContains(t, string(public), "dynamic_rate_factor")
	admin, err := json.Marshal(GroupFromServiceAdmin(g))
	require.NoError(t, err)
	require.Contains(t, string(admin), `"dynamic_rate_factor":2`)
}
