//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSonnet55CacheReadDefaultPricingSources(t *testing.T) {
	const model = "claude-sonnet-5-5"
	const rate = 0.1e-6
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)
	bundled := &PricingService{}
	bundled.pricingData, err = bundled.parsePricingData(body)
	require.NoError(t, err)
	require.NotNil(t, bundled.pricingData[model])
	require.InDelta(t, rate, bundled.pricingData[model].CacheReadInputTokenCost, 1e-15)

	pricingFallback := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	fallback := pricingFallback.GetModelPricing(model)
	require.NotNil(t, fallback)
	require.InDelta(t, rate, fallback.CacheReadInputTokenCost, 1e-15)

	for _, tc := range []struct {
		name    string
		pricing *PricingService
	}{
		{"billing fallback", nil},
		{"pricing fallback", pricingFallback},
		{"bundled catalog", bundled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := NewBillingService(nil, tc.pricing)
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, rate, pricing.CacheReadPricePerToken, 1e-15)
			cost, err := svc.CalculateCost(model, UsageTokens{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 1000}, 0.5)
			require.NoError(t, err)
			require.InDelta(t, 1000*rate, cost.CacheReadCost, 1e-15)
			want := 100*2e-6 + 10*10e-6 + 1000*rate
			require.InDelta(t, want, cost.TotalCost, 1e-15)
			require.InDelta(t, want*0.5, cost.ActualCost, 1e-15)
		})
	}
}

func TestSonnet55CacheReadCustomPricesRemainAuthoritative(t *testing.T) {
	const model = "claude-sonnet-5-5"
	svc := NewBillingService(nil, nil)
	for _, tc := range []struct {
		name  string
		price float64
	}{{"operator price", 0.75e-6}, {"explicitly free", 0}} {
		t.Run(tc.name, func(t *testing.T) {
			price := tc.price
			card := ChannelModelPricing{Platform: PlatformAnthropic, Models: []string{model}, BillingMode: BillingModeToken, CacheReadPrice: &price}
			tokens := UsageTokens{CacheReadTokens: 1000}
			channel, err := svc.GetModelPricingWithChannel(model, &card)
			require.NoError(t, err)
			require.Equal(t, price, channel.CacheReadPricePerToken)
			channelCost, err := svc.calculateCostInternalWithPolicy(model, tokens, 1, "", &card, false)
			require.NoError(t, err)
			require.InDelta(t, 1000*price, channelCost.CacheReadCost, 1e-15)

			group := &Group{ID: 55, Platform: PlatformAnthropic, ModelPricing: []ChannelModelPricing{card}}
			groupCost, err := svc.CalculateTokenCostForRequest(TokenCostRequest{
				Ctx: context.Background(), Model: model, Group: group, Tokens: tokens,
				RateMultiplier: 1, Resolver: NewModelPricingResolver(nil, svc),
			})
			require.NoError(t, err)
			require.InDelta(t, 1000*price, groupCost.CacheReadCost, 1e-15)
			require.InDelta(t, 1000*price, groupCost.TotalCost, 1e-15)
		})
	}
	base, err := svc.GetModelPricing(model)
	require.NoError(t, err)
	require.InDelta(t, 0.1e-6, base.CacheReadPricePerToken, 1e-15, "custom cards must not mutate the shared default")
}
