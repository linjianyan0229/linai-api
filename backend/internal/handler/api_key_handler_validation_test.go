//go:build unit

package handler

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyConcurrencyRequestValidation(t *testing.T) {
	for _, limit := range []int{0, 1, 100000} {
		require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Concurrency: limit}))
		require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Concurrency: &limit}))
	}
	for _, limit := range []int{-1, 100001} {
		require.Error(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Concurrency: limit}))
		require.Error(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Concurrency: &limit}))
	}
	var omitted, cleared UpdateAPIKeyRequest
	require.NoError(t, json.Unmarshal([]byte(`{}`), &omitted))
	require.NoError(t, json.Unmarshal([]byte(`{"concurrency":0}`), &cleared))
	require.Nil(t, omitted.Concurrency)
	require.NotNil(t, cleared.Concurrency)
	require.Zero(t, *cleared.Concurrency)
	for _, payload := range []string{`{"concurrency":1.5}`, `{"concurrency":"3"}`} {
		var req CreateAPIKeyRequest
		require.Error(t, json.Unmarshal([]byte(payload), &req))
	}
}

func TestValidateAPIKeyCreateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(1)
	positiveDays, zeroDays, negativeDays := 1, 0, -1
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{}))
	require.NoError(t, validateAPIKeyCreateRequest(CreateAPIKeyRequest{Quota: &zero, RateLimit5h: &large, ExpiresInDays: &positiveDays}))

	for _, req := range []CreateAPIKeyRequest{
		{Quota: &negative},
		{Quota: &nan},
		{RateLimit5h: &inf},
		{RateLimit1d: &negative},
		{RateLimit7d: &negative},
		{ExpiresInDays: &zeroDays},
		{ExpiresInDays: &negativeDays},
	} {
		require.Error(t, validateAPIKeyCreateRequest(req))
	}
}

func TestValidateAPIKeyUpdateRequest(t *testing.T) {
	zero, large, negative, nan, inf := 0.0, 1e100, -1.0, math.NaN(), math.Inf(-1)
	require.NoError(t, validateAPIKeyUpdateRequest(UpdateAPIKeyRequest{Quota: &zero, RateLimit7d: &large}))

	for _, req := range []UpdateAPIKeyRequest{
		{Quota: &negative},
		{RateLimit5h: &nan},
		{RateLimit1d: &inf},
		{RateLimit7d: &negative},
	} {
		require.Error(t, validateAPIKeyUpdateRequest(req))
	}
}
