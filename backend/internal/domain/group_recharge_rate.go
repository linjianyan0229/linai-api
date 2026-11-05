package domain

// GroupRechargeRateTier replaces the group's base rate once the user's net
// lifetime balance-recharge principal reaches MinAmount (USD).
type GroupRechargeRateTier struct {
	MinAmount      float64 `json:"min_amount"`
	RateMultiplier float64 `json:"rate_multiplier"`
}
