//go:build integration

package repository

import "testing"

func TestGroupTimeRateIntegration(t *testing.T) {
	runGroupTimeRateContract(t, integrationDB)
}
