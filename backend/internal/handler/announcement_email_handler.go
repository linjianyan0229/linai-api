package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AnnouncementEmailHandler struct {
	service *service.AnnouncementEmailService
}

func NewAnnouncementEmailHandler(s *service.AnnouncementEmailService) *AnnouncementEmailHandler {
	return &AnnouncementEmailHandler{service: s}
}

func (h *AnnouncementEmailHandler) Get(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	p, err := h.service.Preference(c.Request.Context(), subject.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, p)
}

func (h *AnnouncementEmailHandler) Update(c *gin.Context) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.Enabled == nil {
		response.BadRequest(c, "enabled must be a boolean")
		return
	}
	p, err := h.service.SetPreference(c.Request.Context(), subject.UserID, *input.Enabled, c.GetHeader("Accept-Language"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, p)
}
