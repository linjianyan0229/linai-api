package service

import (
	"math"
	"sort"
	"time"
	_ "time/tzdata"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

type GroupTimeRatePeriod = domain.GroupTimeRatePeriod

const GroupTimeRateTimezone = "Asia/Shanghai"
const MaxGroupTimeRatePeriods = 48

var groupTimeRateLocation = func() *time.Location {
	location, err := time.LoadLocation(GroupTimeRateTimezone)
	if err != nil {
		return time.FixedZone(GroupTimeRateTimezone, 8*60*60)
	}
	return location
}()

func ValidateGroupTimeRatePeriods(periods []GroupTimeRatePeriod, legacyPeakEnabled bool) error {
	if len(periods) == 0 {
		return nil
	}
	invalid := func(message string) error {
		return infraerrors.BadRequest("INVALID_GROUP_TIME_RATES", message)
	}
	if legacyPeakEnabled {
		return invalid("daily peak/off-peak rates cannot be combined with legacy subscription peak pricing")
	}
	if len(periods) < 2 || len(periods) > MaxGroupTimeRatePeriods {
		return invalid("between 2 and 48 daily periods are required")
	}
	type interval struct{ start, end int }
	intervals := make([]interval, 0, len(periods)+1)
	for _, period := range periods {
		if period.Kind != "" && period.Kind != "peak" && period.Kind != "valley" {
			return invalid("period kind must be peak or valley")
		}
		start, ok := parseMinutes(period.Start)
		if !ok {
			return invalid("period start must be HH:MM")
		}
		end, ok := parseMinutes(period.End)
		if !ok || start == end {
			return invalid("period end must be HH:MM and different from its start")
		}
		rate := period.RateMultiplier
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < 0 || rate > 999999.9999 ||
			!decimal.NewFromFloat(rate).Equal(decimal.NewFromFloat(rate).Round(4)) {
			return invalid("time rates must be between 0 and 999999.9999 with at most four decimal places")
		}
		if start < end {
			intervals = append(intervals, interval{start, end})
		} else {
			intervals = append(intervals, interval{start, 1440})
			if end > 0 {
				intervals = append(intervals, interval{0, end})
			}
		}
	}
	sort.Slice(intervals, func(i, j int) bool { return intervals[i].start < intervals[j].start })
	cursor := 0
	for _, span := range intervals {
		if span.start != cursor {
			return invalid("daily periods must cover 24 hours without gaps or overlaps")
		}
		cursor = span.end
	}
	if cursor != 1440 {
		return invalid("daily periods must cover 24 hours without gaps or overlaps")
	}
	return nil
}

// TimeRateAt selects a period for this request. It is never cached: a request
// crossing a time boundary keeps its supplied pricing instant throughout billing.
func (g *Group) TimeRateAt(now time.Time) (float64, bool) {
	if g == nil || len(g.TimeRatePeriods) < 2 {
		return 1, false
	}
	local := now.In(groupTimeRateLocation)
	return groupTimeRateAt(g.TimeRatePeriods, local)
}

func groupTimeRateAt(periods []GroupTimeRatePeriod, local time.Time) (float64, bool) {
	minute := local.Hour()*60 + local.Minute()
	for _, period := range periods {
		start, okStart := parseMinutes(period.Start)
		end, okEnd := parseMinutes(period.End)
		if !okStart || !okEnd || start == end {
			return 1, false
		}
		if (start < end && minute >= start && minute < end) ||
			(start > end && (minute >= start || minute < end)) {
			return period.RateMultiplier, true
		}
	}
	return 1, false
}

// TimeAdjustedRate uses an absolute period rate without recharge levels. With
// recharge levels it multiplies the resolved base (manual override or earned
// level) by the period coefficient. Disabled schedules preserve existing pricing.
func (g *Group) TimeAdjustedRate(base float64, now time.Time) float64 {
	if g != nil && g.DynamicRateEnabled {
		return base
	}
	rate, enabled := g.TimeRateAt(now)
	if !enabled {
		return base
	}
	if len(g.RechargeRateTiers) == 3 && !g.IsSubscriptionType() {
		return base * rate
	}
	return rate
}

// EffectiveTokenRate also accounts for the pre-existing subscription peak rule.
func (g *Group) EffectiveTokenRate(base float64, now time.Time) float64 {
	return g.TimeAdjustedRate(base, now) * g.PeakMultiplierAt(now)
}

func TimeAdjustedGroupRate(apiKey *APIKey, base float64, now time.Time) float64 {
	if apiKey == nil {
		return base
	}
	return apiKey.Group.TimeAdjustedRate(base, now)
}
