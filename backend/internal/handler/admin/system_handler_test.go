//go:build unit

package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

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
