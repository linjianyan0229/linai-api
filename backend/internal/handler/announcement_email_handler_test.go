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

type announcementEmailHandlerRepo struct {
	service.AnnouncementEmailRepository
	userID  int64
	enabled bool
	calls   int
}

func (r *announcementEmailHandlerRepo) Preference(_ context.Context, userID int64) (*service.AnnouncementEmailPreference, error) {
	r.userID = userID
	return &service.AnnouncementEmailPreference{Email: "owner@example.test", Enabled: r.enabled}, nil
}
func (r *announcementEmailHandlerRepo) SetPreference(_ context.Context, userID int64, enabled bool, _ string) error {
	r.userID = userID
	r.enabled = enabled
	r.calls++
	return nil
}

func TestAnnouncementEmailPreferenceUsesSessionOwnerAndBoolean(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := new(announcementEmailHandlerRepo)
	h := NewAnnouncementEmailHandler(service.NewAnnouncementEmailService(repo, nil, nil))
	router := gin.New()
	router.PUT("/unauthorized", h.Update)
	router.PUT("/user", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) }, h.Update)
	for _, body := range []string{`{}`, `{"enabled":"yes"}`, `{"enabled":null}`} {
		request := httptest.NewRequest("PUT", "/user", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		require.Equal(t, http.StatusBadRequest, rec.Code)
	}
	require.Zero(t, repo.calls)
	request := httptest.NewRequest("PUT", "/user?user_id=99", strings.NewReader(`{"enabled":true,"user_id":99}`))
	request.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), repo.userID)
	require.True(t, repo.enabled)
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("PUT", "/unauthorized", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}
