package service

import (
	"context"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

type GroupRechargeRateTier = domain.GroupRechargeRateTier

// Empty tiers disable the feature. Enabled groups have exactly three levels,
// starting at zero, with increasing thresholds and decreasing absolute rates.
func ValidateGroupRechargeRateTiers(tiers []GroupRechargeRateTier, subscriptionType string) error {
	if len(tiers) == 0 {
		return nil
	}
	invalid := func(message string) error {
		return infraerrors.BadRequest("INVALID_RECHARGE_RATE_TIERS", message)
	}
	if subscriptionType == SubscriptionTypeSubscription {
		return invalid("recharge rate tiers are only available for standard groups")
	}
	if len(tiers) != 3 || tiers[0].MinAmount != 0 {
		return invalid("exactly three recharge levels are required, with the first threshold at 0 USD")
	}
	for i, tier := range tiers {
		if math.IsNaN(tier.MinAmount) || math.IsInf(tier.MinAmount, 0) || tier.MinAmount < 0 || tier.MinAmount > 1e12 {
			return invalid("recharge thresholds must be between 0 and 1000000000000 USD")
		}
		if !decimal.NewFromFloat(tier.MinAmount).Equal(decimal.NewFromFloat(tier.MinAmount).Round(2)) {
			return invalid("recharge thresholds support at most two decimal places")
		}
		if math.IsNaN(tier.RateMultiplier) || math.IsInf(tier.RateMultiplier, 0) || tier.RateMultiplier < 0.5 || tier.RateMultiplier > 1 ||
			!decimal.NewFromFloat(tier.RateMultiplier).Equal(decimal.NewFromFloat(tier.RateMultiplier).Round(4)) {
			return invalid("recharge rates must be between 0.5 and 1 with at most four decimal places")
		}
		if i > 0 && (tier.MinAmount <= tiers[i-1].MinAmount || tier.RateMultiplier >= tiers[i-1].RateMultiplier) {
			return invalid("recharge thresholds must increase and rates must decrease at each level")
		}
	}
	return nil
}

func GroupRechargeLevel(tiers []GroupRechargeRateTier, principal float64) int {
	for i := len(tiers) - 1; i >= 0; i-- {
		if principal >= tiers[i].MinAmount {
			return i + 1
		}
	}
	return 0
}

// Keep administrator-assigned rates separate from derived recharge discounts;
// an admin reading/editing a user's overrides must never persist a derived rate.
type UserGroupRechargeRateRepository interface {
	GetEffectiveByUserID(ctx context.Context, userID int64) (map[int64]float64, error)
	GetRechargePrincipal(ctx context.Context, userID int64) (float64, error)
}

type GroupRechargeProgress struct {
	GroupID         int64                   `json:"group_id"`
	GroupName       string                  `json:"group_name"`
	Tiers           []GroupRechargeRateTier `json:"tiers"`
	Level           int                     `json:"level"`
	RateMultiplier  float64                 `json:"rate_multiplier"`
	HasUserOverride bool                    `json:"has_user_override"`
}

type UserRechargeProgress struct {
	PrincipalUSD float64                 `json:"principal_usd"`
	Groups       []GroupRechargeProgress `json:"groups"`
}

func (s *APIKeyService) GetRechargeProgress(ctx context.Context, userID int64, language string) (*UserRechargeProgress, error) {
	result := &UserRechargeProgress{Groups: []GroupRechargeProgress{}}
	repo, ok := s.userGroupRateRepo.(UserGroupRechargeRateRepository)
	if !ok {
		return result, nil
	}
	groups, err := s.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	result.PrincipalUSD, err = repo.GetRechargePrincipal(ctx, userID)
	if err != nil {
		return nil, err
	}
	overrides, err := s.userGroupRateRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		if len(group.RechargeRateTiers) != 3 || group.IsSubscriptionType() {
			continue
		}
		level := GroupRechargeLevel(group.RechargeRateTiers, result.PrincipalUSD)
		if level == 0 {
			continue
		}
		name, _ := group.I18n.Localize(language, group.Name, group.Description)
		progress := GroupRechargeProgress{
			GroupID: group.ID, GroupName: name, Tiers: group.RechargeRateTiers,
			Level: level, RateMultiplier: group.RechargeRateTiers[level-1].RateMultiplier,
		}
		if rate, exists := overrides[group.ID]; exists {
			progress.RateMultiplier, progress.HasUserOverride = rate, true
		}
		result.Groups = append(result.Groups, progress)
	}
	return result, nil
}
