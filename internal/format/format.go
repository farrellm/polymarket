// Package format renders numbers the way the browser shows them: short enough
// for a column, and precise enough to compare rows by eye.
package format

import "strconv"

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

	// A value that would round to 1000 of a unit belongs to the next one:
	// $999.6K is $1.0M, not $1000K.
	const roundsUp = 999.5
	u := 0
	for u < len(moneyUnits)-1 && v/moneyUnits[u].size >= roundsUp {
		u++
	}

	// The same goes for the decimal place: 99.96 would print as 100.0.
	const oneDecimalBelow = 99.95
	scaled := v / moneyUnits[u].size
	decimals := 0
	if u > 0 && scaled < oneDecimalBelow {
		decimals = 1
	}
	return sign + "$" + strconv.FormatFloat(scaled, 'f', decimals, 64) + moneyUnits[u].suffix
}
