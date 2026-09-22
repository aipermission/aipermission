package timeformat

import "time"

// Layout is fixed-width and preserves nanoseconds so textual database ordering
// remains chronological without collapsing distinct event times.
const Layout = "2006-01-02T15:04:05.000000000Z"

const PreciseLayout = "2006-01-02T15:04:05.000000000Z"

func UTC(value time.Time) string {
	return value.UTC().Format(Layout)
}

func Now() string {
	return UTC(time.Now())
}

// PreciseUTC names call sites where timestamp equality is also a revision
// contract. It intentionally shares UTC's canonical storage representation.
func PreciseUTC(value time.Time) string {
	return value.UTC().Format(PreciseLayout)
}
