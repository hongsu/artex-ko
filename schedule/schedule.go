// Package schedule calculates calendar schedules without depending on the host's local zone.
package schedule

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // Minimal Docker images must support the same IANA zones as development.
)

type Spec struct {
	Kind     string `json:"kind"`
	Date     string `json:"date,omitempty"`
	Time     string `json:"time"`
	Weekday  int    `json:"weekday"` // Sunday=0
	Timezone string `json:"timezone"`
}

func ValidTimezone(name string) bool {
	if name == "" || name == "Local" || len(name) > 128 || strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
func ResolveTimezone(override, env string) (string, string) {
	if ValidTimezone(override) {
		return override, "override"
	}
	if ValidTimezone(env) {
		return env, "environment"
	}
	return "Asia/Seoul", "default"
}
func (s Spec) Validate() error {
	if s.Kind != "once" && s.Kind != "daily" && s.Kind != "weekly" {
		return fmt.Errorf("invalid schedule kind")
	}
	if !ValidTimezone(s.Timezone) {
		return fmt.Errorf("invalid IANA timezone")
	}
	if len(s.Time) != 5 {
		return fmt.Errorf("time must be HH:mm")
	}
	if _, e := time.Parse("15:04", s.Time); e != nil {
		return fmt.Errorf("time must be HH:mm")
	}
	if s.Kind == "weekly" && (s.Weekday < 0 || s.Weekday > 6) {
		return fmt.Errorf("weekday must be 0..6")
	}
	if s.Kind == "once" {
		if _, e := time.Parse("2006-01-02", s.Date); e != nil {
			return fmt.Errorf("invalid date")
		}
	}
	return nil
}

// wallTime chooses the earlier occurrence of a repeated clock time. A nonexistent
// DST time has no occurrence; recurring schedules skip that day.
func wallTime(day time.Time, clock string, loc *time.Location) (time.Time, bool) {
	hhmm, _ := time.Parse("15:04", clock)
	y, m, d := day.Date()
	target := time.Date(y, m, d, hhmm.Hour(), hhmm.Minute(), 0, 0, loc)
	for delta := -180; delta <= 180; delta++ {
		candidate := target.Add(time.Duration(delta) * time.Minute)
		local := candidate.In(loc)
		if local.Year() == y && local.Month() == m && local.Day() == d && local.Hour() == hhmm.Hour() && local.Minute() == hhmm.Minute() {
			return candidate.UTC(), true
		}
	}
	return time.Time{}, false
}

// Next returns the first occurrence strictly after after. Once schedules return
// zero when consumed. Calendar arithmetic preserves local times across DST.
func (s Spec) Next(after time.Time) (time.Time, error) {
	if e := s.Validate(); e != nil {
		return time.Time{}, e
	}
	loc, _ := time.LoadLocation(s.Timezone)
	if s.Kind == "once" {
		day, _ := time.Parse("2006-01-02", s.Date)
		at, ok := wallTime(day, s.Time, loc)
		if !ok {
			return time.Time{}, fmt.Errorf("selected local time does not exist in timezone")
		}
		if !at.After(after) {
			return time.Time{}, nil
		}
		return at, nil
	}
	local := after.In(loc)
	y, m, d := local.Date()
	// Noon avoids a midnight transition shifting the calendar date.
	day := time.Date(y, m, d, 12, 0, 0, 0, loc)
	for i := 0; i < 15; i++ {
		candidateDay := day.AddDate(0, 0, i)
		if s.Kind == "weekly" && int(candidateDay.Weekday()) != s.Weekday {
			continue
		}
		at, ok := wallTime(candidateDay, s.Time, loc)
		if ok && at.After(after) {
			return at, nil
		}
	}
	return time.Time{}, fmt.Errorf("no occurrence in next two weeks")
}
