package admin

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/pkg/sysutil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// SystemHandler exposes local build metadata and service restart.
type SystemHandler struct {
	version string
	lockSvc *service.SystemOperationLockService
	updater systemReleaseUpdater
}

type systemReleaseUpdater interface {
	Check(context.Context, bool) (*service.ReleaseUpdateInfo, error)
	Install(context.Context, string) error
}

func (h *SystemHandler) SetReleaseUpdater(updater *service.ReleaseUpdateService) { h.updater = updater }

func NewSystemHandler(version string, lockSvc *service.SystemOperationLockService) *SystemHandler {
	return &SystemHandler{version: version, lockSvc: lockSvc}
}

// GetVersion reads local build metadata without contacting a release server.
func (h *SystemHandler) GetVersion(c *gin.Context) {
	response.Success(c, gin.H{"version": h.version})
}

func (h *SystemHandler) CheckUpdates(c *gin.Context) {
	if h.updater == nil {
		response.ErrorFrom(c, service.ErrReleaseCheckFailed)
		return
	}
	info, err := h.updater.Check(c.Request.Context(), c.Query("force") == "true")
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, info)
}

func (h *SystemHandler) PerformUpdate(c *gin.Context) {
	var request struct {
		Version string `json:"version" binding:"required,max=64"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		response.ErrorFrom(c, service.ErrReleaseNotAllowed)
		return
	}
	if h.updater == nil {
		response.ErrorFrom(c, service.ErrReleaseCheckFailed)
		return
	}
	operationID := buildSystemOperationID(c, "update:"+request.Version)
	executeAdminIdempotentJSONWithTimeout(c, "admin.system.release_update", request, service.DefaultSystemOperationIdempotencyTTL(), 15*time.Minute, func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		succeeded := false
		defer func() { release("RELEASE_INSTALL_FAILED", succeeded) }()
		if err := h.updater.Install(ctx, request.Version); err != nil {
			return nil, err
		}
		succeeded = true
		return gin.H{"version": request.Version, "need_restart": true, "operation_id": lock.OperationID()}, nil
	})
}

// RestartService restarts the systemd service
// POST /api/v1/admin/system/restart
func (h *SystemHandler) RestartService(c *gin.Context) {
	operationID := buildSystemOperationID(c, "restart")
	payload := gin.H{"operation_id": operationID}
	executeAdminIdempotentJSON(c, "admin.system.restart", payload, service.DefaultSystemOperationIdempotencyTTL(), func(ctx context.Context) (any, error) {
		lock, release, err := h.acquireSystemLock(ctx, operationID)
		if err != nil {
			return nil, err
		}
		succeeded := false
		defer func() {
			release("", succeeded)
		}()

		// Schedule service restart in background after sending response
		// This ensures the client receives the success response before the service restarts
		go func() {
			// Wait a moment to ensure the response is sent
			time.Sleep(500 * time.Millisecond)
			sysutil.RestartServiceAsync()
		}()
		succeeded = true
		return gin.H{
			"message":      "Service restart initiated",
			"operation_id": lock.OperationID(),
		}, nil
	})
}

func (h *SystemHandler) acquireSystemLock(
	ctx context.Context,
	operationID string,
) (*service.SystemOperationLock, func(string, bool), error) {
	if h.lockSvc == nil {
		return nil, nil, service.ErrIdempotencyStoreUnavail
	}
	lock, err := h.lockSvc.Acquire(ctx, operationID)
	if err != nil {
		return nil, nil, err
	}
	release := func(reason string, succeeded bool) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h.lockSvc.Release(releaseCtx, lock, succeeded, reason)
	}
	return lock, release, nil
}

func buildSystemOperationID(c *gin.Context, operation string) string {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		return "sysop-" + operation + "-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	actorScope := "admin:0"
	if subject, ok := middleware2.GetAuthSubjectFromContext(c); ok {
		actorScope = "admin:" + strconv.FormatInt(subject.UserID, 10)
	}
	seed := operation + "|" + actorScope + "|" + c.FullPath() + "|" + key
	hash := service.HashIdempotencyKey(seed)
	if len(hash) > 24 {
		hash = hash[:24]
	}
	return "sysop-" + hash
}
