package format

import "testing"

func TestMoney(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "$0"},
		{0.4, "$0"},
		{12, "$12"},
		{950, "$950"},
		{999.4, "$999"},
		// Rounding up to a thousand moves to the next unit.
		{999.5, "$1.0K"},
		{1234, "$1.2K"},
		{9_800, "$9.8K"},
		{84_200, "$84.2K"},
		// Below a hundred of the unit there is a decimal; from there up, none.
		{99_940, "$99.9K"},
		{99_960, "$100K"},
		{310_000, "$310K"},
		{999_600, "$1.0M"},
		{1_100_000, "$1.1M"},
		{84_200_000, "$84.2M"},
		{412_000_000, "$412M"},
		{3_500_000_000, "$3.5B"},
		{2e12, "$2.0T"},
		// Nothing is larger than the last unit, so it just grows.
		{5e15, "$5000T"},
		{-1500, "-$1.5K"},
	}
	for _, c := range cases {
		if got := Money(c.in); got != c.want {
			t.Errorf("Money(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
