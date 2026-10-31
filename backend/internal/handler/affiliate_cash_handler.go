package handler

import (
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type AffiliateCashHandler struct{ service *service.AffiliateCashService }

func NewAffiliateCashHandler(s *service.AffiliateCashService) *AffiliateCashHandler {
	return &AffiliateCashHandler{service: s}
}

func cashUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "User not authenticated")
		return 0, false
	}
	return subject.UserID, true
}

func cashID(c *gin.Context, param string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(param), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid application ID")
		return 0, false
	}
	return id, true
}

func (h *AffiliateCashHandler) Summary(c *gin.Context) {
	userID, ok := cashUser(c)
	if !ok {
		return
	}
	data, err := h.service.Summary(c.Request.Context(), userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, data)
}

func (h *AffiliateCashHandler) Apply(c *gin.Context) {
	userID, ok := cashUser(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, service.AffiliateCashQRMaxBytes+64*1024)
	if err := c.Request.ParseMultipartForm(service.AffiliateCashQRMaxBytes + 64*1024); err != nil {
		response.BadRequest(c, "请上传不超过 2 MB 的收款码，并填写邮箱和金额")
		return
	}
	if c.Request.MultipartForm == nil {
		response.BadRequest(c, "请使用表单上传收款码")
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	files := c.Request.MultipartForm.File["payment_qr"]
	if len(files) != 1 {
		response.BadRequest(c, "请上传一张收款码")
		return
	}
	file, err := files[0].Open()
	if err != nil {
		response.BadRequest(c, "无法读取收款码")
		return
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, service.AffiliateCashQRMaxBytes+1))
	if err != nil {
		response.BadRequest(c, "无法读取收款码")
		return
	}
	application, err := h.service.Apply(c.Request.Context(), userID, c.PostForm("email"), c.PostForm("amount"), data, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if application.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, application)
}

func (h *AffiliateCashHandler) ListUser(c *gin.Context) {
	userID, ok := cashUser(c)
	if ok {
		h.list(c, userID)
	}
}
func (h *AffiliateCashHandler) ListAdmin(c *gin.Context) { h.list(c, 0) }
func (h *AffiliateCashHandler) list(c *gin.Context, userID int64) {
	page, size := response.ParsePagination(c)
	items, total, err := h.service.List(c.Request.Context(), service.AffiliateCashFilter{UserID: userID, Status: c.Query("status"), Page: page, PageSize: size})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Paginated(c, items, total, page, size)
}

func (h *AffiliateCashHandler) GetUser(c *gin.Context) {
	userID, ok := cashUser(c)
	if ok {
		h.get(c, userID)
	}
}
func (h *AffiliateCashHandler) GetAdmin(c *gin.Context) { h.get(c, 0) }
func (h *AffiliateCashHandler) get(c *gin.Context, userID int64) {
	id, ok := cashID(c, "id")
	if !ok {
		return
	}
	a, err := h.service.Get(c.Request.Context(), id, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, a)
}

func (h *AffiliateCashHandler) QRUser(c *gin.Context) {
	userID, ok := cashUser(c)
	if ok {
		h.qr(c, userID)
	}
}
func (h *AffiliateCashHandler) QRAdmin(c *gin.Context) { h.qr(c, 0) }
func (h *AffiliateCashHandler) qr(c *gin.Context, userID int64) {
	id, ok := cashID(c, "id")
	if !ok {
		return
	}
	data, mime, err := h.service.QR(c.Request.Context(), id, userID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.Data(http.StatusOK, mime, data)
}

func (h *AffiliateCashHandler) Reply(c *gin.Context) {
	adminID, ok := cashUser(c)
	if !ok {
		return
	}
	id, ok := cashID(c, "id")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	var input struct {
		Content  string `json:"content"`
		Decision string `json:"decision"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid reply")
		return
	}
	application, err := h.service.Reply(c.Request.Context(), id, adminID, input.Content, input.Decision, c.GetHeader("Idempotency-Key"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, application)
}

func (h *AffiliateCashHandler) RetryReply(c *gin.Context) {
	id, ok := cashID(c, "id")
	if !ok {
		return
	}
	replyID, ok := cashID(c, "reply_id")
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1024)
	var input struct {
		ConfirmUncertain bool `json:"confirm_uncertain"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		response.BadRequest(c, "Invalid retry")
		return
	}
	application, err := h.service.RetryReply(c.Request.Context(), id, replyID, input.ConfirmUncertain)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	c.Header("Cache-Control", "private, no-store")
	response.Success(c, application)
}
