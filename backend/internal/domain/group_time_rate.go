package domain

// GroupTimeRatePeriod is a daily half-open interval in Asia/Shanghai.
// Kind labels the interval for display; its rate determines billing.
type GroupTimeRatePeriod struct {
	Kind           string  `json:"kind,omitempty"`
	Start          string  `json:"start"`
	End            string  `json:"end"`
	RateMultiplier float64 `json:"rate_multiplier"`
}
