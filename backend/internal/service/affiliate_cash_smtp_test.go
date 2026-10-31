//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type cashSMTPSettings struct {
	SettingRepository
	port int
}

func (r *cashSMTPSettings) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{SettingKeySMTPHost: "127.0.0.1", SettingKeySMTPPort: strconv.Itoa(r.port), SettingKeySMTPUsername: "test", SettingKeySMTPPassword: "test-only", SettingKeySMTPFrom: "noreply@example.test", SettingKeySMTPFromName: "Cash Test", SettingKeySMTPUseTLS: "false"}, nil
}

func TestAffiliateCashReplyUsesConfiguredSMTP(t *testing.T) {
	smtp, port := startFakeSMTPServer(t, false, false)
	email := NewEmailService(&cashSMTPSettings{port: port}, nil)
	repo := &cashRepoStub{reply: &AffiliateCashReply{ID: 1, ApplicationID: 1, Content: "收款资料已确认", Decision: "pending", DeliveryToken: "smtp-attempt", ShouldSend: true}}
	svc := NewAffiliateCashService(repo, email)
	_, err := svc.Reply(context.Background(), 1, 9, "收款资料已确认", "pending", "configured-smtp-123")
	require.NoError(t, err)
	require.True(t, repo.finishSent)
	require.True(t, smtp.sawCommand("MAIL FROM:<NOREPLY@EXAMPLE.TEST>"))
	require.True(t, smtp.sawCommand("RCPT TO:<PAY@EXAMPLE.TEST>"))
	require.True(t, smtp.sawCommand("DATA"))
}
