package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	_ "golang.org/x/image/webp"
)

const AffiliateCashQRMaxBytes = 2 * 1024 * 1024

var (
	ErrAffiliateCashUnavailable = infraerrors.BadRequest("AFFILIATE_CASH_INSUFFICIENT", "申请金额超过最近 24 小时内尚未使用的返利额度")
	ErrAffiliateCashNotFound    = infraerrors.NotFound("AFFILIATE_CASH_NOT_FOUND", "提现申请不存在")
	ErrAffiliateCashResolved    = infraerrors.Conflict("AFFILIATE_CASH_RESOLVED", "该申请已处理，请刷新后查看")
	ErrAffiliateCashFrozen      = infraerrors.Conflict("AFFILIATE_CASH_FROZEN", "返利仍在冻结期，暂时不能登记打款")
	ErrAffiliateCashDelivery    = infraerrors.Conflict("AFFILIATE_CASH_DELIVERY_BUSY", "邮件已发送或正在发送，请刷新后查看")
	affiliateCashAmountPattern  = regexp.MustCompile(`^[0-9]{1,10}(\.[0-9]{1,8})?$`)
)

type AffiliateCashSummary struct {
	EligibleAmount string     `json:"eligible_amount"`
	PendingAmount  string     `json:"pending_amount"`
	NextExpiresAt  *time.Time `json:"next_expires_at"`
	ServerTime     time.Time  `json:"server_time"`
}

type AffiliateCashApplication struct {
	ID              int64                `json:"id"`
	UserID          int64                `json:"user_id"`
	Username        string               `json:"username"`
	Email           string               `json:"email"`
	Amount          string               `json:"amount"`
	Currency        string               `json:"currency"`
	Status          string               `json:"status"`
	PayoutNotBefore time.Time            `json:"payout_not_before"`
	CreatedAt       time.Time            `json:"created_at"`
	ResolvedAt      *time.Time           `json:"resolved_at"`
	Replies         []AffiliateCashReply `json:"replies,omitempty"`
	RequestHash     string               `json:"-"`
	Replayed        bool                 `json:"-"`
}

type AffiliateCashReply struct {
	ID             int64      `json:"id"`
	ApplicationID  int64      `json:"application_id"`
	Content        string     `json:"content"`
	Decision       string     `json:"decision"`
	DeliveryStatus string     `json:"delivery_status"`
	CreatedAt      time.Time  `json:"created_at"`
	SentAt         *time.Time `json:"sent_at"`
	RequestHash    string     `json:"-"`
	DeliveryToken  string     `json:"-"`
	ShouldSend     bool       `json:"-"`
}

type AffiliateCashCreate struct {
	UserID      int64
	Email       string
	Amount      string
	QR          []byte
	QRType      string
	OperationID string
	RequestHash string
}

type AffiliateCashReplyInput struct {
	ApplicationID int64
	AdminID       int64
	Content       string
	Decision      string
	OperationID   string
	RequestHash   string
	DeliveryToken string
}

type AffiliateCashFilter struct {
	UserID   int64
	Status   string
	Page     int
	PageSize int
}

type AffiliateCashRepository interface {
	Summary(context.Context, int64) (*AffiliateCashSummary, error)
	Create(context.Context, AffiliateCashCreate) (*AffiliateCashApplication, error)
	List(context.Context, AffiliateCashFilter) ([]AffiliateCashApplication, int64, error)
	Get(context.Context, int64, int64) (*AffiliateCashApplication, error)
	QR(context.Context, int64, int64) ([]byte, string, error)
	Reply(context.Context, AffiliateCashReplyInput) (*AffiliateCashReply, error)
	ClaimReplyRetry(context.Context, int64, int64, string, bool) (*AffiliateCashReply, error)
	FinishReply(context.Context, int64, string, bool) error
}

type affiliateCashMailer interface {
	SendEmail(context.Context, string, string, string) error
}

type AffiliateCashService struct {
	repo   AffiliateCashRepository
	mailer affiliateCashMailer
}

func NewAffiliateCashService(repo AffiliateCashRepository, email *EmailService) *AffiliateCashService {
	return &AffiliateCashService{repo: repo, mailer: email}
}

func (s *AffiliateCashService) Summary(ctx context.Context, userID int64) (*AffiliateCashSummary, error) {
	return s.repo.Summary(ctx, userID)
}

func cashOperation(scope, key string) (string, error) {
	key, err := NormalizeIdempotencyKey(key)
	if err != nil {
		return "", err
	}
	if key == "" {
		return "", ErrIdempotencyKeyRequired
	}
	return cashHash([]byte(scope + "\x00" + key)), nil
}

func cashHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s *AffiliateCashService) Apply(ctx context.Context, userID int64, email, amount string, qr []byte, key string) (*AffiliateCashApplication, error) {
	operation, err := cashOperation("affiliate.cash.apply", key)
	if err != nil {
		return nil, err
	}
	email = strings.TrimSpace(email)
	address, err := mail.ParseAddress(email)
	if err != nil || len(email) > 254 || address.Address != email || strings.ContainsAny(email, "\r\n") || !strings.Contains(email, "@") {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_EMAIL_INVALID", "请填写有效的收信邮箱")
	}
	amount = strings.TrimSpace(amount)
	if !affiliateCashAmountPattern.MatchString(amount) {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_AMOUNT_INVALID", "请输入正数 USD 金额，最多 8 位小数")
	}
	value, err := decimal.NewFromString(amount)
	if err != nil || !value.IsPositive() {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_AMOUNT_INVALID", "请输入正数 USD 金额，最多 8 位小数")
	}
	if len(qr) == 0 || len(qr) > AffiliateCashQRMaxBytes {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_QR_INVALID", "请上传不超过 2 MB 的 PNG、JPEG 或 WebP 收款码")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(qr))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16000000 || (format != "png" && format != "jpeg" && format != "webp") {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_QR_INVALID", "收款码必须为有效图片，宽高不超过 4096 像素")
	}
	// Decode the complete image too: a valid header alone must not admit a
	// truncated or malformed upload to the private payment-code endpoint.
	if _, _, err := image.Decode(bytes.NewReader(qr)); err != nil {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_QR_INVALID", "收款码图片已损坏")
	}
	payload, _ := json.Marshal([]string{email, value.String(), cashHash(qr)})
	return s.repo.Create(ctx, AffiliateCashCreate{UserID: userID, Email: email, Amount: value.String(), QR: qr, QRType: "image/" + format, OperationID: operation, RequestHash: cashHash(payload)})
}

func (s *AffiliateCashService) List(ctx context.Context, filter AffiliateCashFilter) ([]AffiliateCashApplication, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.PageSize < 1 {
		filter.PageSize = 20
	}
	if filter.PageSize > 100 {
		filter.PageSize = 100
	}
	if filter.Status != "" && filter.Status != "pending" && filter.Status != "paid" && filter.Status != "rejected" {
		return nil, 0, infraerrors.BadRequest("AFFILIATE_CASH_STATUS_INVALID", "无效的申请状态")
	}
	return s.repo.List(ctx, filter)
}

func (s *AffiliateCashService) Get(ctx context.Context, id, userID int64) (*AffiliateCashApplication, error) {
	return s.repo.Get(ctx, id, userID)
}

func (s *AffiliateCashService) QR(ctx context.Context, id, userID int64) ([]byte, string, error) {
	return s.repo.QR(ctx, id, userID)
}

func (s *AffiliateCashService) Reply(ctx context.Context, id, adminID int64, content, decision, key string) (*AffiliateCashApplication, error) {
	op, err := cashOperation("affiliate.cash.reply", key)
	if err != nil {
		return nil, err
	}
	content = strings.TrimSpace(content)
	if !utf8.ValidString(content) || utf8.RuneCountInString(content) < 1 || utf8.RuneCountInString(content) > 4000 {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_REPLY_INVALID", "请填写回复内容，最多 4000 字")
	}
	if decision != "pending" && decision != "paid" && decision != "rejected" {
		return nil, infraerrors.BadRequest("AFFILIATE_CASH_STATUS_INVALID", "无效的处理状态")
	}
	payload, _ := json.Marshal([]string{strconv.FormatInt(id, 10), content, decision})
	reply, err := s.repo.Reply(ctx, AffiliateCashReplyInput{ApplicationID: id, AdminID: adminID, Content: content, Decision: decision, OperationID: op, RequestHash: cashHash(payload), DeliveryToken: uuid.NewString()})
	if err != nil {
		return nil, err
	}
	if reply.ShouldSend {
		if err := s.deliver(ctx, reply); err != nil {
			return nil, err
		}
	}
	return s.repo.Get(ctx, id, 0)
}

func (s *AffiliateCashService) RetryReply(ctx context.Context, id, replyID int64, confirmUncertain bool) (*AffiliateCashApplication, error) {
	reply, err := s.repo.ClaimReplyRetry(ctx, id, replyID, uuid.NewString(), confirmUncertain)
	if err != nil {
		return nil, err
	}
	if err := s.deliver(ctx, reply); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, id, 0)
}

func (s *AffiliateCashService) deliver(ctx context.Context, reply *AffiliateCashReply) error {
	// Once the reply has been durably claimed, client disconnects must not
	// abandon its delivery bookkeeping. SMTP already has bounded I/O timeouts.
	deliveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Minute)
	defer cancel()
	application, err := s.repo.Get(deliveryCtx, reply.ApplicationID, 0)
	if err != nil {
		return err
	}
	status := map[string]string{"pending": "待处理 / Pending", "paid": "已打款 / Paid", "rejected": "已驳回 / Rejected"}[application.Status]
	body := fmt.Sprintf("<h2>返利提现申请 / Rebate withdrawal #%d</h2><p>USD %s · %s</p><p style=\"white-space:pre-wrap\">%s</p>", application.ID, html.EscapeString(application.Amount), html.EscapeString(status), html.EscapeString(reply.Content))
	err = s.mailer.SendEmail(deliveryCtx, application.Email, fmt.Sprintf("返利提现申请回复 / Withdrawal #%d", application.ID), body)
	// Failed mail is persisted and returned in the application; payout state
	// remains committed. Explicit retry sends the same saved reply only.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer finishCancel()
	return s.repo.FinishReply(finishCtx, reply.ID, reply.DeliveryToken, err == nil)
}
