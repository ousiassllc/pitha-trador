package atoms

import "time"

// JST is the fixed Asia/Tokyo offset every SSR time label is rendered in.
// A fixed zone (not time.Local / time.LoadLocation) keeps the output
// independent of the host timezone and of tzdata availability, and the
// repositories hand times back as UTC (sqlutil.ParseTime), so formatting
// them directly would print the TSE open 09:00 JST as 00:00Z.
var JST = time.FixedZone("JST", 9*60*60)

// TimeLayoutJST is the one layout for SSR timestamps; its literal "JST"
// states the zone, so a reader never has to guess it.
const TimeLayoutJST = "2006-01-02 15:04:05 JST"

// FormatJST renders t in JST, e.g. 2026-10-05T00:30:00Z ->
// `2026-10-05 09:30:00 JST`. API (JSON) output stays RFC 3339; this is for
// operator-facing HTML only.
func FormatJST(t time.Time) string {
	return t.In(JST).Format(TimeLayoutJST)
}
