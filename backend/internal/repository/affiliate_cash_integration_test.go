//go:build integration

package repository

import (
	"context"
	"testing"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAffiliateCashUsesRealMigratedSchema(t *testing.T) {
	ctx := context.Background()
	tx := testEntTx(t)
	ctx = dbent.NewTxContext(ctx, tx)
	client := tx.Client()
	owner := mustCreateAffiliateWithdrawUser(t, client, "cash-owner")
	inviter := mustCreateAffiliateWithdrawUser(t, client, "cash-invitee")
	admin := mustCreateAffiliateWithdrawUser(t, client, "cash-admin")
	legacy := NewAffiliateRepository(client, integrationDB)
	_, err := legacy.EnsureUserAffiliate(ctx, owner.ID)
	require.NoError(t, err)
	_, err = legacy.AccrueQuota(ctx, owner.ID, inviter.ID, 10.25, 0, nil)
	require.NoError(t, err)
	cash := NewAffiliateCashRepository(client)
	application, err := cash.Create(ctx, service.AffiliateCashCreate{UserID: owner.ID, Email: "cash@example.test", Amount: "4.25", QR: []byte("private-png"), QRType: "image/png", OperationID: "integration-cash", RequestHash: "integration-cash"})
	require.NoError(t, err)
	transferred, _, err := legacy.TransferQuotaToBalance(ctx, owner.ID)
	require.NoError(t, err)
	require.Equal(t, 6.0, transferred)
	_, err = cash.Reply(ctx, service.AffiliateCashReplyInput{ApplicationID: application.ID, AdminID: admin.ID, Content: "Paid offline", Decision: "paid", OperationID: "integration-paid", RequestHash: "integration-paid", DeliveryToken: "integration-token"})
	require.NoError(t, err)
	detail, err := cash.Get(ctx, application.ID, owner.ID)
	require.NoError(t, err)
	require.Equal(t, "paid", detail.Status)
	require.Len(t, detail.Replies, 1)
	_, _, err = cash.QR(ctx, application.ID, inviter.ID)
	require.ErrorIs(t, err, service.ErrAffiliateCashNotFound)
}
