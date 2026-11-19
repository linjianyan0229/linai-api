//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyConcurrencyValidation(t *testing.T) {
	for _, limit := range []int{0, 1, MaxAPIKeyConcurrency} {
		require.NoError(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{Concurrency: limit}))
		require.NoError(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{Concurrency: &limit}))
	}
	for _, limit := range []int{-1, MaxAPIKeyConcurrency + 1} {
		require.Error(t, validateCreateAPIKeyRequest(CreateAPIKeyRequest{Concurrency: limit}))
		require.Error(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{Concurrency: &limit}))
	}
	require.NoError(t, validateUpdateAPIKeyRequest(UpdateAPIKeyRequest{}))
}

func TestAPIKeyServiceCreatePersistsConcurrency(t *testing.T) {
	for _, limit := range []int{0, 2, MaxAPIKeyConcurrency} {
		repo, cache := newCreateLimitStubs()
		svc := newCreateLimitService(repo, cache, 0, 0)
		key, err := svc.Create(context.Background(), 7, CreateAPIKeyRequest{Name: "concurrency", Concurrency: limit})
		require.NoError(t, err)
		require.Equal(t, limit, key.Concurrency)
		require.Len(t, repo.created, 1)
		require.Equal(t, limit, repo.created[0].Concurrency)
	}
}

func TestAPIKeyServiceUpdateConcurrencyOmittedAndClear(t *testing.T) {
	zero, positive := 0, 3
	for _, tt := range []struct {
		name  string
		limit *int
		want  int
		mask  APIKeyUpdateFields
	}{
		{name: "omitted", want: 7},
		{name: "set", limit: &positive, want: 3, mask: APIKeyUpdateFields{Concurrency: true}},
		{name: "clear", limit: &zero, want: 0, mask: APIKeyUpdateFields{Concurrency: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newUpdateFieldsAPIKeyService(&APIKey{ID: 1, UserID: 7, Concurrency: 7, Status: StatusActive})
			key, err := svc.Update(context.Background(), 1, 7, UpdateAPIKeyRequest{Concurrency: tt.limit})
			require.NoError(t, err)
			require.Equal(t, tt.want, key.Concurrency)
			require.Equal(t, []APIKeyUpdateFields{tt.mask}, repo.updateFields)
		})
	}
}
