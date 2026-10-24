//go:build unit

package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemVersionUsesLocalBuildInformation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewSystemHandler("linai-local-build", nil)
	router := gin.New()
	router.GET("/version", handler.GetVersion)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/version", nil))
	require.Equal(t, http.StatusOK, recorder.Code)
	require.JSONEq(t, `{"code":0,"message":"success","data":{"version":"linai-local-build"}}`, recorder.Body.String())
}

type systemReleaseUpdaterStub struct {
	checks   int
	installs int
	force    bool
}

func (s *systemReleaseUpdaterStub) Check(_ context.Context, force bool) (*service.ReleaseUpdateInfo, error) {
	s.checks++
	s.force = force
	return &service.ReleaseUpdateInfo{Repository: "linjianyan0229/linai-api", CurrentVersion: "1.0.0", LatestVersion: "1.1.0", HasUpdate: true}, nil
}
func (s *systemReleaseUpdaterStub) Install(context.Context, string) error { s.installs++; return nil }

func TestSystemReleaseDetectionNeverInstallsAndVersionRemainsLocal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	updater := &systemReleaseUpdaterStub{}
	handler := NewSystemHandler("1.0.0", nil)
	handler.updater = updater
	router := gin.New()
	router.GET("/version", handler.GetVersion)
	router.GET("/check-updates", handler.CheckUpdates)
	router.POST("/update", handler.PerformUpdate)
	version := httptest.NewRecorder()
	router.ServeHTTP(version, httptest.NewRequest(http.MethodGet, "/version", nil))
	require.Zero(t, updater.checks)
	check := httptest.NewRecorder()
	router.ServeHTTP(check, httptest.NewRequest(http.MethodGet, "/check-updates?force=true", nil))
	require.Equal(t, http.StatusOK, check.Code)
	require.True(t, updater.force)
	require.Equal(t, 1, updater.checks)
	require.Zero(t, updater.installs)
	invalid := httptest.NewRecorder()
	router.ServeHTTP(invalid, httptest.NewRequest(http.MethodPost, "/update", nil))
	require.Equal(t, http.StatusBadRequest, invalid.Code)
	require.Zero(t, updater.installs)
}
