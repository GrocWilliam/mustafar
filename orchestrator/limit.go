package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the image is FROM scratch: the reset time's zone must resolve without /usr/share/zoneinfo
)

// limitWait is how long to wait when a usage limit gives no readable reset time.
const limitWait = 30 * time.Minute

// usageLimit matches Claude Code's "You've hit your 5-hour limit · resets 1:40pm (UTC)".
var usageLimit = regexp.MustCompile(`(?i)hit your .*limit(?:.*resets\D*?(\d{1,2})(?::(\d{2}))?\s*(am|pm)\s*\(([^)]+)\))?`)

// limitReset tells whether an agent's final message says the Claude usage limit is reached, and until when.
// shortcut: a reset with a date ("resets Nov 5, 9am") is read as the next 9am; the run then hits the limit again and waits again.
func limitReset(final string, now time.Time) (time.Time, bool) {
	m := usageLimit.FindStringSubmatch(final)
	if m == nil {
		return time.Time{}, false
	}
	loc, err := time.LoadLocation(m[4])
	if m[1] == "" || err != nil {
		return now.Add(limitWait), true
	}
	hour, _ := strconv.Atoi(m[1])
	minute, _ := strconv.Atoi(m[2])
	hour %= 12
	if strings.EqualFold(m[3], "pm") {
		hour += 12
	}
	local := now.In(loc)
	reset := time.Date(local.Year(), local.Month(), local.Day(), hour, minute, 0, 0, loc)
	if !reset.After(now) {
		reset = reset.AddDate(0, 0, 1)
	}
	return reset.Add(time.Minute), true // a minute of margin past the announced reset
}
