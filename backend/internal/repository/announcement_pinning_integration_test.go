//go:build integration

package repository

import "testing"

func TestAnnouncementPinningIntegration(t *testing.T) {
	runAnnouncementPinningContract(t, integrationDB, integrationEntClient)
}
