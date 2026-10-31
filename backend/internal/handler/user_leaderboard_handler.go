package handler

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type UserLeaderboardHandler struct {
	service *service.UserLeaderboardService
}

func NewUserLeaderboardHandler(s *service.UserLeaderboardService) *UserLeaderboardHandler {
	return &UserLeaderboardHandler{service: s}
}

func (h *UserLeaderboardHandler) Get(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	if len(c.QueryArray("model")) > 1 {
		response.BadRequest(c, "Select a single model")
		return
	}
	snapshot, err := h.service.Get(c.Request.Context(), c.Query("model"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, snapshot)
}
