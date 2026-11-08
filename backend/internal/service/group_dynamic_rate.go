package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/gin-gonic/gin"
)

// DynamicGroupPricing is an immutable quote for one upstream attempt/WS turn.
// Never attach it to a shared Account, Group, or user:group cache entry.
type DynamicGroupPricing struct {
	GroupID         int64
	AccountID       int64
	UpstreamRate    float64
	AccountCostRate float64
	Factor          float64
	Rate            float64
	PricingAt       time.Time
}

type GroupDynamicRateAvailability struct {
	EligibleAccounts int      `json:"eligible_accounts"`
	UpstreamMin      *float64 `json:"upstream_min"`
	UpstreamMax      *float64 `json:"upstream_max"`
}

func ValidateGroupDynamicRate(enabled bool, factor float64, platform string) error {
	if !enabled && factor == 0 {
		return nil
	}
	if enabled && platform != PlatformOpenAI {
		return infraerrors.BadRequest("DYNAMIC_RATE_PLATFORM_UNSUPPORTED", "dynamic group rates require the OpenAI platform")
	}
	if math.IsNaN(factor) || math.IsInf(factor, 0) || factor <= 0 || factor > 9999.9999 || math.Abs(factor*1e4-math.Round(factor*1e4)) > 1e-6 {
		return infraerrors.BadRequest("INVALID_DYNAMIC_RATE_FACTOR", "dynamic rate factor must be positive, at most 9999.9999, with at most four decimal places")
	}
	return nil
}

// DynamicUpstreamBillingRate requires a successful, fresh, continuously enabled
// probe. A manually entered account rate is never evidence of an upstream quote.
func DynamicUpstreamBillingRate(account *Account, now time.Time) (float64, bool) {
	if account == nil || account.Status != StatusActive || account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey {
		return 0, false
	}
	if enabled, _ := account.Extra[UpstreamBillingProbeEnabledExtraKey].(bool); !enabled {
		return 0, false
	}
	snapshot := decodeUpstreamBillingProbeSnapshot(account.Extra)
	if snapshot == nil || snapshot.Status != UpstreamBillingProbeStatusOK || snapshot.ReceivedAt == nil {
		return 0, false
	}
	expires := snapshot.FreshUntil
	if expires == nil && snapshot.NextProbeAt.After(*snapshot.ReceivedAt) {
		v := snapshot.ReceivedAt.Add(2 * snapshot.NextProbeAt.Sub(*snapshot.ReceivedAt))
		expires = &v
	}
	if expires == nil || !now.Before(*expires) {
		return 0, false
	}
	rate, ok := openAIFreshUpstreamBillingRate(account, now)
	return rate, ok && !math.IsNaN(rate) && !math.IsInf(rate, 0) && rate >= 0 && rate <= 100
}

func DynamicRateAvailability(accounts []*Account, now time.Time) GroupDynamicRateAvailability {
	var result GroupDynamicRateAvailability
	for _, account := range accounts {
		if account == nil || account.Status != StatusActive {
			continue
		}
		rate, ok := DynamicUpstreamBillingRate(account, now)
		if !ok {
			continue
		}
		result.EligibleAccounts++
		if result.UpstreamMin == nil || rate < *result.UpstreamMin {
			v := rate
			result.UpstreamMin = &v
		}
		if result.UpstreamMax == nil || rate > *result.UpstreamMax {
			v := rate
			result.UpstreamMax = &v
		}
	}
	return result
}

func dynamicBillingGroup(ctx context.Context) *Group {
	if group, ok := ctx.Value(dynamicBillingGroupContextKey{}).(*Group); ok {
		return group
	}
	if group := gatewayTokenRequestBillingGroupFromContext(ctx); group != nil {
		return group
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	return group
}

func dynamicRateQuote(group *Group, account *Account, now time.Time) (*DynamicGroupPricing, error) {
	if group == nil || !group.DynamicRateEnabled {
		return nil, nil
	}
	if err := ValidateGroupDynamicRate(true, group.DynamicRateFactor, group.Platform); err != nil {
		return nil, err
	}
	upstream, ok := DynamicUpstreamBillingRate(account, now)
	if !ok {
		return nil, fmt.Errorf("dynamic upstream rate is unavailable or expired")
	}
	rate := upstream * group.DynamicRateFactor
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 999999.9999 {
		return nil, fmt.Errorf("dynamic group rate exceeds supported range")
	}
	costRate := account.BillingRateMultiplier()
	if math.IsNaN(costRate) || math.IsInf(costRate, 0) || costRate < 0 {
		return nil, fmt.Errorf("invalid configured account cost rate")
	}
	return &DynamicGroupPricing{GroupID: group.ID, AccountID: account.ID, UpstreamRate: upstream, AccountCostRate: costRate, Factor: group.DynamicRateFactor, Rate: rate, PricingAt: now}, nil
}

// FreezeDynamicGroupPricing must run before forwarding. Usage consumes this
// quote even when a long response finishes after probe expiry or a price change.
func FreezeDynamicGroupPricing(ctx context.Context, account *Account) (*DynamicGroupPricing, error) {
	quote, err := dynamicRateQuote(dynamicBillingGroup(ctx), account, time.Now())
	if err == nil && quote != nil {
		if gate, _ := ctx.Value(openAIProfitControlGateCtxKey{}).(*openAIProfitControlGate); gate != nil && gate.dynamicFactor > 0 && profitControlOverThreshold(quote.AccountCostRate, quote.Rate*gate.dynamicMargin) {
			return nil, fmt.Errorf("dynamic quote does not meet the profit threshold")
		}
	}
	return quote, err
}

func dynamicAccountEligible(ctx context.Context, account *Account) bool {
	_, err := dynamicRateQuote(dynamicBillingGroup(ctx), account, time.Now())
	return err == nil
}

func dynamicUsageRate(group *Group, account *Account, quote *DynamicGroupPricing) (float64, bool, error) {
	if quote == nil {
		if group != nil && group.DynamicRateEnabled {
			return 0, false, fmt.Errorf("dynamic billing requires a frozen upstream quote")
		}
		return 0, false, nil
	}
	if group == nil || account == nil || quote.GroupID != group.ID || quote.AccountID != account.ID || quote.PricingAt.IsZero() ||
		math.IsNaN(quote.AccountCostRate) || math.IsInf(quote.AccountCostRate, 0) || quote.AccountCostRate < 0 || quote.Factor <= 0 || quote.UpstreamRate < 0 ||
		math.IsNaN(quote.Rate) || math.IsInf(quote.Rate, 0) || quote.Rate < 0 || quote.Rate > 999999.9999 || quote.Rate != quote.UpstreamRate*quote.Factor {
		return 0, false, fmt.Errorf("dynamic billing quote does not match this request")
	}
	return quote.Rate, true, nil
}

func (s *adminServiceImpl) GetGroupDynamicRateAvailability(ctx context.Context, groupID int64) (*GroupDynamicRateAvailability, error) {
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if group.Platform != PlatformOpenAI {
		return nil, infraerrors.BadRequest("DYNAMIC_RATE_PLATFORM_UNSUPPORTED", "dynamic group rates require the OpenAI platform")
	}
	ids, err := s.groupRepo.GetAccountIDsByGroupIDs(ctx, []int64{groupID})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &GroupDynamicRateAvailability{}, nil
	}
	accounts, err := s.accountRepo.GetByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := DynamicRateAvailability(accounts, time.Now())
	return &result, nil
}

func (s *adminServiceImpl) validateDynamicRateActivation(ctx context.Context, group *Group) error {
	if err := ValidateGroupDynamicRate(group.DynamicRateEnabled, group.DynamicRateFactor, group.Platform); err != nil {
		return err
	}
	if !group.DynamicRateEnabled {
		return nil
	}
	availability, err := s.GetGroupDynamicRateAvailability(ctx, group.ID)
	if err != nil {
		return err
	}
	if availability.EligibleAccounts == 0 {
		return infraerrors.BadRequest("DYNAMIC_RATE_UPSTREAM_UNAVAILABLE", "enable upstream billing probes and obtain a fresh rate from a bound OpenAI API key account before enabling dynamic pricing")
	}
	return nil
}

func (g *Group) DynamicRateFactorOrDefault() float64 {
	if g == nil || (!g.DynamicRateEnabled && g.DynamicRateFactor == 0) {
		return 2
	}
	return g.DynamicRateFactor
}

func prepareForwardDynamicPricing(ctx context.Context, c *gin.Context, account *Account) (*DynamicGroupPricing, error) {
	if c != nil {
		c.Set(dynamicPricingGinKey, (*DynamicGroupPricing)(nil))
	}
	// The WS handler freezes every turn, including the first frame. Its HTTP
	// bridge carries the connection context, which can have an older group.
	if frozen, _ := ctx.Value(externallyFrozenDynamicPricingKey{}).(bool); frozen {
		return nil, nil
	}
	quote, err := FreezeDynamicGroupPricing(ctx, account)
	if err != nil {
		return nil, &UpstreamFailoverError{StatusCode: 503, ResponseBody: []byte(`{"error":{"type":"api_error","message":"Upstream dynamic rate is unavailable; please retry later"}}`)}
	}
	if c != nil {
		c.Set(dynamicPricingGinKey, quote)
	}
	return quote, nil
}

type externallyFrozenDynamicPricingKey struct{}
type dynamicBillingGroupContextKey struct{}

// WithExternallyFrozenDynamicPricing is only for WS ingress with mandatory
// per-turn pricing hooks. The handler attaches that turn's quote to its result.
func WithExternallyFrozenDynamicPricing(ctx context.Context) context.Context {
	return context.WithValue(ctx, externallyFrozenDynamicPricingKey{}, true)
}

func WithDynamicBillingGroup(ctx context.Context, group *Group) context.Context {
	return context.WithValue(ctx, dynamicBillingGroupContextKey{}, group)
}

func attachForwardDynamicPricing(result *OpenAIForwardResult, quote *DynamicGroupPricing) {
	if result != nil && result.DynamicGroupPricing == nil {
		result.DynamicGroupPricing = quote
	}
}

func inflightDynamicRateCeiling(snapshot *SchedulerSnapshotService) func(context.Context, *APIKey) (float64, bool) {
	return func(ctx context.Context, key *APIKey) (float64, bool) {
		if snapshot == nil || key == nil || key.Group == nil {
			return 0, false
		}
		accounts, _, err := snapshot.ListSchedulableAccounts(ctx, key.GroupID, PlatformOpenAI, false)
		if err != nil {
			return 0, false
		}
		maxRate, found, now := 0.0, false, time.Now()
		for i := range accounts {
			quote, err := dynamicRateQuote(key.Group, &accounts[i], now)
			if err != nil || quote == nil {
				continue
			}
			if !found || quote.Rate > maxRate {
				maxRate = quote.Rate
			}
			found = true
		}
		return maxRate, found
	}
}

const dynamicPricingGinKey = "openai_dynamic_group_pricing"

func DynamicGroupPricingForAccount(c *gin.Context, accountID int64) *DynamicGroupPricing {
	if c == nil {
		return nil
	}
	value, _ := c.Get(dynamicPricingGinKey)
	quote, _ := value.(*DynamicGroupPricing)
	if quote == nil || quote.AccountID != accountID {
		return nil
	}
	return quote
}
