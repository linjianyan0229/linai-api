//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type announcementSMTPSettings struct {
	SettingRepository
	port int
}

func (r *announcementSMTPSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{SettingKeySMTPHost: "127.0.0.1", SettingKeySMTPPort: strconv.Itoa(r.port), SettingKeySMTPUsername: "test", SettingKeySMTPPassword: "test-only", SettingKeySMTPFrom: "noreply@example.test", SettingKeySMTPUseTLS: "false", SettingKeySiteName: "LinAI"}, nil
}

func TestAnnouncementEmailUsesConfiguredSMTP(t *testing.T) {
	smtp, port := startFakeSMTPServer(t, false, false)
	settings := &announcementSMTPSettings{port: port}
	repo := &announcementMailRepoStub{results: map[int64]string{}}
	svc := NewAnnouncementEmailService(repo, NewEmailService(settings, nil), settings)
	svc.deliver(context.Background(), &AnnouncementEmailTask{ID: 1, Token: "local-smtp-test", Email: "recipient@example.test", Locale: "zh", Title: "Example", Content: "Local test only"})
	require.Equal(t, "sent", repo.results[1])
	require.True(t, smtp.sawCommand("MAIL FROM:<NOREPLY@EXAMPLE.TEST>"))
	require.True(t, smtp.sawCommand("RCPT TO:<RECIPIENT@EXAMPLE.TEST>"))
	require.True(t, smtp.sawCommand("DATA"))
}
