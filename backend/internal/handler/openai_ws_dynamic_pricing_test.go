package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupDynamicWSTurnPricingIsolation(t *testing.T) {
	var keys openAIWSTurnBillingAPIKeys
	first := &service.DynamicGroupPricing{GroupID: 7, AccountID: 3, Rate: .16}
	second := &service.DynamicGroupPricing{GroupID: 7, AccountID: 3, Rate: .2}
	keys.setQuote(1, first)
	keys.setQuote(2, second) // next BeforeTurn may run before previous AfterTurn
	previousResult, currentResult := &service.OpenAIForwardResult{}, &service.OpenAIForwardResult{}
	keys.attachQuote(1, previousResult)
	keys.attachQuote(2, currentResult)
	require.Same(t, first, previousResult.DynamicGroupPricing)
	require.Same(t, second, currentResult.DynamicGroupPricing)
	keys.setQuote(3, nil) // administrator disabled the mode for this turn
	bridgeResult := &service.OpenAIForwardResult{DynamicGroupPricing: first}
	keys.attachQuote(3, bridgeResult)
	require.Nil(t, bridgeResult.DynamicGroupPricing)
	require.Same(t, first, previousResult.DynamicGroupPricing)
	require.Same(t, second, currentResult.DynamicGroupPricing)
	require.Len(t, keys.quotes, 2, "connection lifetime does not grow pricing memory")
}
