package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type leaderboardHandlerRepo struct {
	calls int
	model string
}

func (r *leaderboardHandlerRepo) Top(_ context.Context, model string) ([]service.UserLeaderboardEntry, error) {
	r.calls++
	r.model = model
	return []service.UserLeaderboardEntry{{Username: "Alice", TotalTokens: 100, TotalQuota: "0.00000001"}}, nil
}
func (*leaderboardHandlerRepo) Models(context.Context) ([]string, error) {
	return []string{"gpt-test"}, nil
}

func TestUserLeaderboardRequiresLoginAndSingleModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := new(leaderboardHandlerRepo)
	h := NewUserLeaderboardHandler(service.NewUserLeaderboardService(repo))
	router := gin.New()
	router.GET("/unauth", h.Get)
	router.GET("/leaderboard", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1}) }, h.Get)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/unauth", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Zero(t, repo.calls)
	for _, query := range []string{"?model=a&model=b", "?model=" + strings.Repeat("x", 101)} {
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", "/leaderboard"+query, nil))
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/leaderboard?model=gpt-test&limit=999&user_id=999", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "gpt-test", repo.model)
	require.NotContains(t, rec.Body.String(), "email")
	require.NotContains(t, rec.Body.String(), "user_id")
	require.Contains(t, rec.Body.String(), `"limit":20`)
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
}
