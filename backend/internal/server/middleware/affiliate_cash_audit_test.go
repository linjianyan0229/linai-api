package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAffiliateCashAuditOmitsPaymentDetailsAndRecordsPrivateReads(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repository := new(auditCaptureRepository)
	audit := service.NewAuditLogService(repository, nil)
	audit.Start()
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
	for _, path := range []string{"/api/v1/user/aff/withdrawals", "/api/v1/admin/affiliates/withdrawals/:id/replies"} {
		router.POST(path, func(c *gin.Context) { c.Status(http.StatusOK) })
	}
	router.GET("/api/v1/admin/affiliates/withdrawals/:id/payment-qr", func(c *gin.Context) { c.Status(http.StatusOK) })
	for _, path := range []string{"/api/v1/user/aff/withdrawals", "/api/v1/admin/affiliates/withdrawals/1/replies"} {
		request := httptest.NewRequest("POST", path, bytes.NewBufferString(`{"email":"private@example.test","content":"private payout details","payment_qr":"private image"}`))
		request.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, request)
		require.Equal(t, http.StatusOK, rec.Code)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/admin/affiliates/withdrawals/1/payment-qr", nil))
	audit.Stop()
	repository.mu.Lock()
	defer repository.mu.Unlock()
	require.Len(t, repository.logs, 3)
	for _, entry := range repository.logs {
		require.NotContains(t, entry.RequestBody, "private")
		if entry.Method == "GET" {
			require.Equal(t, "admin.affiliate_cash.payment_qr.read", entry.Action)
		}
	}
}
