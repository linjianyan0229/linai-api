package service

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"golang.org/x/sync/singleflight"
)

const UserLeaderboardLimit = 20

type UserLeaderboardEntry struct {
	Rank        int    `json:"rank"`
	Username    string `json:"username"`
	TotalTokens int64  `json:"total_tokens"`
	TotalQuota  string `json:"total_quota"`
}

type UserLeaderboardSnapshot struct {
	Items     []UserLeaderboardEntry `json:"items"`
	Models    []string               `json:"models"`
	Model     string                 `json:"model"`
	UpdatedAt time.Time              `json:"updated_at"`
	Limit     int                    `json:"limit"`
}

type UserLeaderboardRepository interface {
	Top(context.Context, string) ([]UserLeaderboardEntry, error)
	Models(context.Context) ([]string, error)
}

type UserLeaderboardService struct {
	repo     UserLeaderboardRepository
	mu       sync.Mutex
	cache    map[string]UserLeaderboardSnapshot
	models   []string
	modelsAt time.Time
	requests singleflight.Group
}

func NewUserLeaderboardService(repo UserLeaderboardRepository) *UserLeaderboardService {
	return &UserLeaderboardService{repo: repo, cache: make(map[string]UserLeaderboardSnapshot)}
}

func (s *UserLeaderboardService) Get(ctx context.Context, model string) (*UserLeaderboardSnapshot, error) {
	model = strings.TrimSpace(model)
	if !utf8.ValidString(model) || len(model) > 100 || strings.IndexFunc(model, unicode.IsControl) >= 0 {
		return nil, infraerrors.BadRequest("LEADERBOARD_MODEL_INVALID", "model must be a single model name up to 100 bytes")
	}
	result := s.requests.DoChan(model, func() (any, error) {
		s.mu.Lock()
		cached, ok := s.cache[model]
		s.mu.Unlock()
		if ok && time.Since(cached.UpdatedAt) < time.Minute {
			return cached, nil
		}
		// Viewer disconnects do not cancel the shared read for other viewers.
		queryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
		defer cancel()
		items, err := s.repo.Top(queryCtx, model)
		if err != nil {
			return nil, err
		}
		models, err := s.getModels(queryCtx)
		if err != nil {
			return nil, err
		}
		if len(items) > UserLeaderboardLimit {
			items = items[:UserLeaderboardLimit]
		}
		if items == nil {
			items = make([]UserLeaderboardEntry, 0)
		}
		for i := range items {
			items[i].Rank = i + 1
			items[i].Username = strings.TrimSpace(items[i].Username)
		}
		snapshot := UserLeaderboardSnapshot{Items: items, Models: models, Model: model, Limit: UserLeaderboardLimit, UpdatedAt: time.Now().UTC()}
		s.mu.Lock()
		// Bound cache growth when visitors query many different model strings.
		if len(s.cache) >= 128 {
			clear(s.cache)
		}
		s.cache[model] = snapshot
		s.mu.Unlock()
		return snapshot, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-result:
		if result.Err != nil {
			return nil, result.Err
		}
		snapshot := result.Val.(UserLeaderboardSnapshot)
		snapshot.Items = append([]UserLeaderboardEntry{}, snapshot.Items...)
		snapshot.Models = append([]string{}, snapshot.Models...)
		return &snapshot, nil
	}
}

func (s *UserLeaderboardService) getModels(ctx context.Context) ([]string, error) {
	result, err, _ := s.requests.Do("\x00models", func() (any, error) {
		s.mu.Lock()
		models, at := s.models, s.modelsAt
		s.mu.Unlock()
		if !at.IsZero() && time.Since(at) < time.Minute {
			return models, nil
		}
		models, err := s.repo.Models(ctx)
		if err != nil {
			return nil, err
		}
		if models == nil {
			models = make([]string, 0)
		}
		s.mu.Lock()
		s.models = models
		s.modelsAt = time.Now()
		s.mu.Unlock()
		return models, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]string), nil
}
