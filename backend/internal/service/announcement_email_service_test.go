package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

type announcementMailRepoStub struct {
	AnnouncementEmailRepository
	preference AnnouncementEmailPreference
	tasks      []*AnnouncementEmailTask
	results    map[int64]string
	mu         sync.Mutex
	setCalls   int
}

func (r *announcementMailRepoStub) Preference(context.Context, int64) (*AnnouncementEmailPreference, error) {
	p := r.preference
	return &p, nil
}
func (r *announcementMailRepoStub) SetPreference(_ context.Context, _ int64, enabled bool, _ string) error {
	r.preference.Enabled = enabled
	r.setCalls++
	return nil
}
func (r *announcementMailRepoStub) Plan(context.Context, int) (bool, error) { return false, nil }
func (r *announcementMailRepoStub) Claim(context.Context) (*AnnouncementEmailTask, error) {
	if len(r.tasks) == 0 {
		return nil, nil
	}
	task := r.tasks[0]
	r.tasks = r.tasks[1:]
	return task, nil
}
func (r *announcementMailRepoStub) Finish(_ context.Context, id int64, _ string, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results[id] = status
	return nil
}

type announcementMailSenderStub struct {
	calls         int
	subject, body string
	err           error
}

func (s *announcementMailSenderStub) SendEmail(_ context.Context, _, subject, body string) error {
	s.calls++
	s.subject = subject
	s.body = body
	return s.err
}

type announcementMailSettings struct{ SettingRepository }

func (*announcementMailSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{SettingKeySiteName: "LinAI", SettingKeyFrontendURL: "https://website.example.test"}, nil
}

func TestAnnouncementEmailPreferenceRequiresRealAccountEmail(t *testing.T) {
	for _, email := range []string{"linuxdo-id@linuxdo-connect.invalid", "a@example.invalid", "admin@linai.local", "bad", "a@example.test\r\nBcc:x@y.test"} {
		repo := &announcementMailRepoStub{preference: AnnouncementEmailPreference{Email: email}}
		svc := NewAnnouncementEmailService(repo, nil, nil)
		_, err := svc.SetPreference(context.Background(), 1, true, "zh")
		require.Error(t, err)
		require.Zero(t, repo.setCalls)
		_, err = svc.SetPreference(context.Background(), 1, false, "zh")
		require.NoError(t, err, "users can always opt out")
	}
	repo := &announcementMailRepoStub{preference: AnnouncementEmailPreference{Email: "user@example.test"}}
	svc := NewAnnouncementEmailService(repo, nil, nil)
	p, err := svc.SetPreference(context.Background(), 1, true, "zh-CN")
	require.NoError(t, err)
	require.True(t, p.Enabled)
	require.True(t, p.Available)
}

func TestAnnouncementEmailFiltersAudienceAndEscapesBody(t *testing.T) {
	task := &AnnouncementEmailTask{ID: 7, Token: "lease", Email: "user@example.test", Locale: "zh", Title: "Hello <script>", Content: "<img src=x onerror=alert(1)>\nNew announcement", Balance: 5, Targeting: AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{{AllOf: []AnnouncementCondition{{Type: "balance", Operator: "gte", Value: 10}}}}}}
	repo := &announcementMailRepoStub{results: map[int64]string{}}
	sender := new(announcementMailSenderStub)
	svc := &AnnouncementEmailService{repo: repo, mailer: sender, settings: &announcementMailSettings{}}
	svc.deliver(context.Background(), task)
	require.Zero(t, sender.calls)
	require.Equal(t, "skipped", repo.results[7])
	task.Balance = 20
	svc.deliver(context.Background(), task)
	require.Equal(t, 1, sender.calls)
	require.Equal(t, "sent", repo.results[7])
	require.Contains(t, sender.body, "&lt;img")
	require.NotContains(t, sender.body, "<img src=x")
	require.NotContains(t, sender.body, "<script>")
	require.Contains(t, sender.body, "https://website.example.test/profile")
	require.Contains(t, sender.body, "sent by LinAI")
	require.NotContains(t, sender.body, "{{site_name}}")
	require.Contains(t, sender.subject, "网站公告更新")
	sender.err = errors.New("test SMTP failure")
	svc.deliver(context.Background(), task)
	require.Equal(t, "pending", repo.results[7], "durable retry is separate from announcement publication")
}

func TestAnnouncementEmailRunOnceHonorsSubscriptionRules(t *testing.T) {
	rule := AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{{AllOf: []AnnouncementCondition{{Type: "subscription", Operator: "in", GroupIDs: []int64{9}}}}}}
	repo := &announcementMailRepoStub{tasks: []*AnnouncementEmailTask{
		{ID: 1, Email: "one@example.test", GroupIDs: []int64{8}, Targeting: rule},
		{ID: 2, Email: "two@example.test", GroupIDs: []int64{9}, Targeting: rule},
		{ID: 3, Email: "oauth@oidc-connect.invalid"},
	}, results: map[int64]string{}}
	sender := new(announcementMailSenderStub)
	svc := &AnnouncementEmailService{repo: repo, mailer: sender, settings: &announcementMailSettings{}}
	require.NoError(t, svc.runOnce(context.Background()))
	require.Equal(t, map[int64]string{1: "skipped", 2: "sent", 3: "skipped"}, repo.results)
	require.Equal(t, 1, sender.calls)
}
