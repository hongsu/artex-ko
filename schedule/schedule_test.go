package schedule

import (
	"testing"
	"time"
)

func instant(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}
func TestNextWallClock(t *testing.T) {
	cases := []struct {
		name        string
		spec        Spec
		after, want string
	}{
		{"once", Spec{Kind: "once", Date: "2026-10-28", Time: "09:00", Timezone: "Asia/Seoul"}, "2026-10-27T00:00:00Z", "2026-10-28T00:00:00Z"},
		{"daily", Spec{Kind: "daily", Time: "09:00", Timezone: "Asia/Seoul"}, "2026-10-28T00:00:00Z", "2026-10-29T00:00:00Z"},
		{"weekly", Spec{Kind: "weekly", Time: "09:00", Weekday: 1, Timezone: "Asia/Seoul"}, "2026-10-27T00:00:00Z", "2026-11-02T00:00:00Z"},
		{"spring gap skipped", Spec{Kind: "daily", Time: "02:30", Timezone: "America/New_York"}, "2026-03-07T08:00:00Z", "2026-03-09T06:30:00Z"},
		{"fall earlier occurrence", Spec{Kind: "daily", Time: "01:30", Timezone: "America/New_York"}, "2026-10-31T08:00:00Z", "2026-11-01T05:30:00Z"},
		{"fall never twice", Spec{Kind: "daily", Time: "01:30", Timezone: "America/New_York"}, "2026-11-01T05:45:00Z", "2026-11-02T06:30:00Z"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, e := c.spec.Next(instant(c.after))
			if e != nil || !got.Equal(instant(c.want)) {
				t.Fatalf("got %v %v; want %s", got, e, c.want)
			}
		})
	}
}
func TestInvalidSchedules(t *testing.T) {
	for _, s := range []Spec{
		{Kind: "once", Date: "2026-02-30", Time: "09:00", Timezone: "UTC"},
		{Kind: "once", Date: "2026-03-08", Time: "02:30", Timezone: "America/New_York"},
		{Kind: "daily", Time: "25:00", Timezone: "UTC"},
		{Kind: "daily", Time: "09:00", Timezone: "Local"},
		{Kind: "weekly", Time: "09:00", Weekday: 7, Timezone: "UTC"},
	} {
		if _, e := s.Next(instant("2026-01-01T00:00:00Z")); e == nil {
			t.Fatalf("accepted %+v", s)
		}
	}
}
func TestTimezonePrecedence(t *testing.T) {
	for _, c := range []struct{ override, env, want, source string }{
		{"Europe/London", "Asia/Seoul", "Europe/London", "override"},
		{"", "UTC", "UTC", "environment"}, {"", "bad/zone", "Asia/Seoul", "default"}, {"", "", "Asia/Seoul", "default"},
	} {
		got, source := ResolveTimezone(c.override, c.env)
		if got != c.want || source != c.source {
			t.Fatalf("%s/%s", got, source)
		}
	}
}
