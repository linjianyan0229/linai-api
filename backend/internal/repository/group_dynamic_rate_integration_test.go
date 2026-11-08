//go:build integration

package repository

import "testing"

func TestGroupDynamicRateIntegration(t *testing.T) { runGroupDynamicRateContract(t, integrationDB) }
