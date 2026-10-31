package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
)

type cashRepoStub struct {
	AffiliateCashRepository
	create      AffiliateCashCreate
	createCalls int
	reply       *AffiliateCashReply
	finishCalls int
	finishSent  bool
	retryCalls  int
}

func (r *cashRepoStub) Create(_ context.Context, in AffiliateCashCreate) (*AffiliateCashApplication, error) {
	r.create = in
	r.createCalls++
	return &AffiliateCashApplication{ID: 1, Email: in.Email, Amount: in.Amount}, nil
}
func (r *cashRepoStub) Get(context.Context, int64, int64) (*AffiliateCashApplication, error) {
	return &AffiliateCashApplication{ID: 1, Email: "pay@example.test", Amount: "12.00000001", Replies: []AffiliateCashReply{*r.reply}}, nil
}
func (r *cashRepoStub) Reply(context.Context, AffiliateCashReplyInput) (*AffiliateCashReply, error) {
	return r.reply, nil
}
func (r *cashRepoStub) ClaimReplyRetry(context.Context, int64, int64, string, bool) (*AffiliateCashReply, error) {
	r.retryCalls++
	return r.reply, nil
}
func (r *cashRepoStub) FinishReply(_ context.Context, _ int64, _ string, sent bool) error {
	r.finishCalls++
	r.finishSent = sent
	return nil
}

type cashMailerStub struct {
	calls int
	body  string
	err   error
}

func (m *cashMailerStub) SendEmail(_ context.Context, to, subject, body string) error {
	m.calls++
	m.body = body
	return m.err
}

func cashTestPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 4, 4))))
	return data.Bytes()
}

func TestAffiliateCashApplyValidatesMoneyIdentityAndImage(t *testing.T) {
	qr := cashTestPNG(t)
	tests := []struct {
		name, email, amount, key string
		qr                       []byte
	}{
		{"no key", "a@example.test", "1", "", qr},
		{"header injection", "a@example.test\r\nBcc:x@y.test", "1", "cash-key-123", qr},
		{"display name", "A <a@example.test>", "1", "cash-key-123", qr},
		{"zero", "a@example.test", "0", "cash-key-123", qr},
		{"negative", "a@example.test", "-1", "cash-key-123", qr},
		{"precision", "a@example.test", "0.000000001", "cash-key-123", qr},
		{"exponent", "a@example.test", "1e3", "cash-key-123", qr},
		{"overflow", "a@example.test", "10000000000", "cash-key-123", qr},
		{"svg", "a@example.test", "1", "cash-key-123", []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
		{"truncated", "a@example.test", "1", "cash-key-123", qr[:len(qr)/2]},
		{"large", "a@example.test", "1", "cash-key-123", make([]byte, AffiliateCashQRMaxBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := new(cashRepoStub)
			s := NewAffiliateCashService(repo, nil)
			_, err := s.Apply(context.Background(), 1, test.email, test.amount, test.qr, test.key)
			require.Error(t, err)
			require.Zero(t, repo.createCalls)
		})
	}
	repo := new(cashRepoStub)
	s := NewAffiliateCashService(repo, nil)
	_, err := s.Apply(context.Background(), 1, "a@example.test", "0001.00000001", qr, "cash-key-123")
	require.NoError(t, err)
	require.Equal(t, "1.00000001", repo.create.Amount)
	require.Equal(t, "image/png", repo.create.QRType)
	hash := repo.create.RequestHash
	_, err = s.Apply(context.Background(), 1, "a@example.test", "1.000000010", qr, "cash-key-123")
	require.Error(t, err)
	_, err = s.Apply(context.Background(), 1, "a@example.test", "1.00000001", qr, "cash-key-123")
	require.NoError(t, err)
	require.Equal(t, hash, repo.create.RequestHash)
}

func TestAffiliateCashMailFailureIsSavedAndReplayDoesNotSend(t *testing.T) {
	repo := &cashRepoStub{reply: &AffiliateCashReply{ID: 7, ApplicationID: 1, Content: "<script>alert('x')</script>\n付款完成", Decision: "paid", DeliveryToken: "token", ShouldSend: true}}
	mailer := &cashMailerStub{err: errors.New("smtp password sensitive")}
	s := &AffiliateCashService{repo: repo, mailer: mailer}
	result, err := s.Reply(context.Background(), 1, 9, repo.reply.Content, "paid", "reply-key-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1, mailer.calls)
	require.Equal(t, 1, repo.finishCalls)
	require.False(t, repo.finishSent)
	require.NotContains(t, mailer.body, "<script>")
	require.Contains(t, mailer.body, "&lt;script&gt;")
	repo.reply.ShouldSend = false
	_, err = s.Reply(context.Background(), 1, 9, repo.reply.Content, "paid", "reply-key-123")
	require.NoError(t, err)
	require.Equal(t, 1, mailer.calls, "idempotent retry must not send another email")
	mailer.err = nil
	_, err = s.RetryReply(context.Background(), 1, 7, false)
	require.NoError(t, err)
	require.Equal(t, 2, mailer.calls)
	require.True(t, repo.finishSent)
}
