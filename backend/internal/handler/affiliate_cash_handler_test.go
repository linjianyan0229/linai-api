package handler

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/adminaccess"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type cashHandlerRepo struct {
	service.AffiliateCashRepository
	owner int64
	calls int
}

func (r *cashHandlerRepo) QR(_ context.Context, id, userID int64) ([]byte, string, error) {
	r.owner = userID
	r.calls++
	return []byte("private-image"), "image/png", nil
}

func TestAffiliateCashQRUsesAuthenticatedOwnerAndPrivateHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := new(cashHandlerRepo)
	h := NewAffiliateCashHandler(service.NewAffiliateCashService(repo, nil))
	router := gin.New()
	router.GET("/unauth/:id", h.QRUser)
	router.GET("/user/:id", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) }, h.QRUser)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/unauth/1", nil))
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Zero(t, repo.calls)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/user/1?user_id=9", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(42), repo.owner)
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
	require.Contains(t, rec.Header().Get("Content-Security-Policy"), "sandbox")
	require.Equal(t, "private-image", rec.Body.String())
}

func TestAffiliateCashUploadRejectsInvalidAndOversizedBodies(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAffiliateCashHandler(service.NewAffiliateCashService(new(cashHandlerRepo), nil))
	router := gin.New()
	router.POST("/apply", func(c *gin.Context) { c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42}) }, h.Apply)
	var payload bytes.Buffer
	writer := multipart.NewWriter(&payload)
	part, err := writer.CreateFormFile("payment_qr", "huge.png")
	require.NoError(t, err)
	_, err = part.Write(make([]byte, service.AffiliateCashQRMaxBytes+65537))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	request := httptest.NewRequest("POST", "/apply", &payload)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	request = httptest.NewRequest("POST", "/apply", strings.NewReader("email=a%40example.test"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, request)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestAffiliateCashAdminEndpointsDoNotInheritUnrelatedKeyPermissions(t *testing.T) {
	policy := adminaccess.Policy{GlobalPermissions: []string{"payment.read", "payment.refund", "users.read", "users.balance.write"}}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/admin/affiliates/withdrawals"}, {"GET", "/api/v1/admin/affiliates/withdrawals/:id"}, {"GET", "/api/v1/admin/affiliates/withdrawals/:id/payment-qr"}, {"POST", "/api/v1/admin/affiliates/withdrawals/:id/replies"}, {"POST", "/api/v1/admin/affiliates/withdrawals/:id/replies/:reply_id/retry"}} {
		require.False(t, adminaccess.RouteAllowed(policy, route.method, route.path))
	}
}
