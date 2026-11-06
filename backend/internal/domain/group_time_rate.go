package domain

// GroupTimeRatePeriod is a daily half-open interval in the server timezone.
// Two complementary periods represent peak and off-peak pricing.
type GroupTimeRatePeriod struct {
	Start          string  `json:"start"`
	End            string  `json:"end"`
	RateMultiplier float64 `json:"rate_multiplier"`
}
