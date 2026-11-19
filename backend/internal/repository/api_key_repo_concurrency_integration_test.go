//go:build integration

package repository

import "github.com/Wei-Shaw/sub2api/internal/service"

func (s *APIKeyRepoSuite) TestConcurrencyUpdateEnqueuesAuthInvalidation() {
	user := s.mustCreateUser("key-concurrency-outbox@example.com")
	key := &service.APIKey{UserID: user.ID, Key: "key-concurrency-outbox", Name: "limited", Status: service.StatusActive}
	s.Require().NoError(s.repo.Create(s.ctx, key))
	count := func() int {
		rows, err := s.client.QueryContext(s.ctx, `SELECT count(*) FROM auth_cache_invalidation_outbox
			WHERE cache_key = encode(sha256(convert_to($1, 'UTF8')), 'hex')`, key.Key)
		s.Require().NoError(err)
		defer rows.Close()
		s.Require().True(rows.Next())
		var n int
		s.Require().NoError(rows.Scan(&n))
		s.Require().NoError(rows.Err())
		return n
	}
	before := count()
	key.Concurrency = 3
	s.Require().NoError(s.repo.Update(s.ctx, key, service.APIKeyUpdateFields{Concurrency: true}))
	s.Require().Equal(before+1, count(), "a concurrency change must invalidate cached authentication across instances")
	got, err := s.repo.GetByKeyForAuth(s.ctx, key.Key)
	s.Require().NoError(err)
	s.Require().Equal(3, got.Concurrency)
}

func (s *APIKeyRepoSuite) TestConcurrencyDatabaseConstraint() {
	user := s.mustCreateUser("key-concurrency-constraint@example.com")
	key := &service.APIKey{UserID: user.ID, Key: "key-concurrency-constraint", Name: "limited", Status: service.StatusActive}
	s.Require().NoError(s.repo.Create(s.ctx, key))
	_, err := s.client.ExecContext(s.ctx, "UPDATE api_keys SET concurrency = -1 WHERE id = $1", key.ID)
	s.Require().ErrorContains(err, "api_keys_concurrency_valid")
}
