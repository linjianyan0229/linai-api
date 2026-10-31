package service

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type AnnouncementEmailPreference struct {
	Enabled   bool   `json:"enabled"`
	Email     string `json:"email"`
	Available bool   `json:"available"`
}

type AnnouncementEmailTask struct {
	ID        int64
	Token     string
	Email     string
	Locale    string
	Title     string
	Content   string
	Targeting AnnouncementTargeting
	Balance   float64
	GroupIDs  []int64
}

type AnnouncementEmailRepository interface {
	Preference(context.Context, int64) (*AnnouncementEmailPreference, error)
	SetPreference(context.Context, int64, bool, string) error
	Plan(context.Context, int) (bool, error)
	Claim(context.Context) (*AnnouncementEmailTask, error)
	Finish(context.Context, int64, string, string) error
}

type announcementEmailSender interface {
	SendEmail(context.Context, string, string, string) error
}

type AnnouncementEmailService struct {
	repo     AnnouncementEmailRepository
	mailer   announcementEmailSender
	settings SettingRepository
	cancel   context.CancelFunc
	wg       sync.WaitGroup
}

func NewAnnouncementEmailService(repo AnnouncementEmailRepository, email *EmailService, settings SettingRepository) *AnnouncementEmailService {
	return &AnnouncementEmailService{repo: repo, mailer: email, settings: settings}
}

func announcementEmailAvailable(email string) bool {
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || strings.ContainsAny(email, "\r\n") {
		return false
	}
	at := strings.LastIndexByte(email, '@')
	if at < 1 {
		return false
	}
	host := strings.ToLower(email[at+1:])
	return strings.Contains(host, ".") && !strings.HasSuffix(host, ".invalid") && !strings.HasSuffix(host, ".local") && host != "invalid"
}

func (s *AnnouncementEmailService) Preference(ctx context.Context, userID int64) (*AnnouncementEmailPreference, error) {
	p, err := s.repo.Preference(ctx, userID)
	if err != nil {
		return nil, err
	}
	p.Available = announcementEmailAvailable(p.Email)
	return p, nil
}

func (s *AnnouncementEmailService) SetPreference(ctx context.Context, userID int64, enabled bool, locale string) (*AnnouncementEmailPreference, error) {
	p, err := s.Preference(ctx, userID)
	if err != nil {
		return nil, err
	}
	if enabled && !p.Available {
		return nil, infraerrors.BadRequest("ANNOUNCEMENT_EMAIL_REQUIRED", "请先在账号资料中绑定可接收邮件的邮箱")
	}
	if err := s.repo.SetPreference(ctx, userID, enabled, normalizeNotificationLocale(locale)); err != nil {
		return nil, err
	}
	return s.Preference(ctx, userID)
}

func (s *AnnouncementEmailService) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			if err := s.runOnce(ctx); err != nil && ctx.Err() == nil {
				slog.Warn("announcement email cycle failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *AnnouncementEmailService) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *AnnouncementEmailService) runOnce(ctx context.Context) error {
	for i := 0; i < 5; i++ {
		more, err := s.repo.Plan(ctx, 200)
		if err != nil {
			return err
		}
		if !more {
			break
		}
	}
	// Bound both fanout and SMTP concurrency independently of the audience size.
	var workers sync.WaitGroup
	slots := make(chan struct{}, 2)
	for i := 0; i < 20; i++ {
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			workers.Wait()
			return ctx.Err()
		}
		task, err := s.repo.Claim(ctx)
		if err != nil {
			<-slots
			workers.Wait()
			return err
		}
		if task == nil {
			<-slots
			break
		}
		workers.Add(1)
		go func(task *AnnouncementEmailTask) {
			defer workers.Done()
			defer func() { <-slots }()
			s.deliver(ctx, task)
		}(task)
	}
	workers.Wait()
	return nil
}

func (s *AnnouncementEmailService) deliver(ctx context.Context, task *AnnouncementEmailTask) {
	status := "skipped"
	groups := make(map[int64]struct{}, len(task.GroupIDs))
	for _, id := range task.GroupIDs {
		groups[id] = struct{}{}
	}
	if announcementEmailAvailable(task.Email) && task.Targeting.Matches(task.Balance, groups) {
		subject, body, err := s.message(ctx, task)
		if err == nil {
			err = s.mailer.SendEmail(ctx, task.Email, subject, body)
		}
		status = "sent"
		if err != nil {
			status = "pending"
			// Do not log recipients, SMTP details or announcement bodies.
			slog.Warn("announcement email delivery deferred", "delivery_id", task.ID)
		}
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := s.repo.Finish(finishCtx, task.ID, task.Token, status); err != nil {
		slog.Warn("announcement email completion failed", "delivery_id", task.ID)
	}
}

func (s *AnnouncementEmailService) message(ctx context.Context, task *AnnouncementEmailTask) (string, string, error) {
	values, err := s.settings.GetMultiple(ctx, []string{SettingKeySiteName, SettingKeyFrontendURL, SettingKeyAPIBaseURL})
	if err != nil {
		return "", "", err
	}
	name := strings.TrimSpace(values[SettingKeySiteName])
	if name == "" {
		name = defaultSiteName
	}
	title := "Announcement update"
	hint := "Sign in to read the full announcement. You can turn off announcement emails in Profile settings."
	action := "Open website"
	if task.Locale == notificationEmailLocaleChinese {
		title = "网站公告更新"
		hint = "登录网站可查看完整公告。您可以在个人资料的“公告邮件推送”中关闭此类邮件。"
		action = "打开网站"
	}
	content := []rune(task.Content)
	if len(content) > 6000 {
		content = append(content[:6000], []rune("\n…")...)
	}
	body := fmt.Sprintf("<h3>%s</h3><div style=\"white-space:pre-wrap;word-break:break-word\">%s</div><p class=\"muted\">%s</p>", html.EscapeString(task.Title), html.EscapeString(string(content)), hint)
	for _, key := range []string{SettingKeyFrontendURL, SettingKeyAPIBaseURL} {
		address := strings.TrimRight(strings.TrimSpace(values[key]), "/")
		parsed, err := url.Parse(address)
		if err == nil && parsed.Host != "" && (parsed.Scheme == "https" || parsed.Scheme == "http") && parsed.User == nil {
			body += fmt.Sprintf("<p><a class=\"button\" href=\"%s\">%s</a></p>", html.EscapeString(address+"/profile"), action)
			break
		}
	}
	return fmt.Sprintf("[%s] %s: %s", name, title, task.Title), notificationEmailCard("#0d9488", title, body), nil
}
