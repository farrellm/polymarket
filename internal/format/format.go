// Package format renders numbers the way the browser shows them: short enough
// for a column, and precise enough to compare rows by eye.
package format

import (
	"strconv"
	"time"
)

// moneyUnits are the suffixes Money scales by, smallest first.
var moneyUnits = []struct {
	size   float64
	suffix string
}{
	{1, ""},
	{1e3, "K"},
	{1e6, "M"},
	{1e9, "B"},
	{1e12, "T"},
}

// Money renders an amount in dollars in at most six characters: $950, $9.8K,
// $84.2M, $310M. It keeps one decimal place below a hundred of the unit and
// none from there up, which is about three significant figures throughout.
func Money(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return sign + "$" + scaled(v)
}

// Quantity renders a number of shares the way Money renders dollars, without
// the sign of one: 950, 9.8K, 291K.
func Quantity(v float64) string {
	sign := ""
	if v < 0 {
		sign, v = "-", -v
	}
	return sign + scaled(v)
}

// scaled renders a figure that is not negative in at most five characters.
func scaled(v float64) string {

	// A value that would round to 1000 of a unit belongs to the next one:
	// $999.6K is $1.0M, not $1000K.
	const roundsUp = 999.5
	u := 0
	for u < len(moneyUnits)-1 && v/moneyUnits[u].size >= roundsUp {
		u++
	}

	// The same goes for the decimal place: 99.96 would print as 100.0.
	const oneDecimalBelow = 99.95
	in := v / moneyUnits[u].size
	decimals := 0
	if u > 0 && in < oneDecimalBelow {
		decimals = 1
	}
	return strconv.FormatFloat(in, 'f', decimals, 64) + moneyUnits[u].suffix
}

// Price renders a price between 0 and 1 in cents, to the tenth of a cent the
// finest tick allows: 66.5¢, 7.0¢, 0.3¢.
func Price(p float64) string {
	return strconv.FormatFloat(p*100, 'f', 1, 64) + "¢"
}

// Delta renders a change in price in cents, always with its sign, so that the
// direction does not rest on colour alone: +3.5¢, -1.0¢. No change at all is
// 0.0¢.
func Delta(d float64) string {
	s := strconv.FormatFloat(d*100, 'f', 1, 64)
	switch {
	case s == "0.0" || s == "-0.0":
		return "0.0¢"
	case d > 0:
		return "+" + s + "¢"
	}
	return s + "¢"
}

// spans are the units Until counts in, largest first.
var spans = []struct {
	length time.Duration
	suffix string
}{
	{365 * 24 * time.Hour, "y"},
	{30 * 24 * time.Hour, "mo"},
	{24 * time.Hour, "d"},
	{time.Hour, "h"},
	{time.Minute, "m"},
}

// Until renders how far off t is from now in the largest unit that fits,
// rounded down: 2y, 3mo, 12d, 5h, 40m. A moment gone by reads the same with a
// minus sign, and one less than a minute either way is "now".
func Until(t, now time.Time) string {
	d := t.Sub(now)
	sign := ""
	if d < 0 {
		sign, d = "-", -d
	}
	for _, s := range spans {
		if d >= s.length {
			return sign + strconv.FormatInt(int64(d/s.length), 10) + s.suffix
		}
	}
	return "now"
}

// Clock renders the moment of something recent, in now's time zone: the time
// of day if it was today, else the day and the time to the minute.
func Clock(t, now time.Time) string {
	t = t.In(now.Location())
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04:05")
	}
	if t.Year() == now.Year() {
		return t.Format("Jan 2 15:04")
	}
	return t.Format(time.DateOnly)
}
