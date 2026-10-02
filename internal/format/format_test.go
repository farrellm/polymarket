package format

import (
	"testing"
	"time"
)

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

func TestPrice(t *testing.T) {
	for in, want := range map[float64]string{
		0: "0.0¢", 0.003: "0.3¢", 0.07: "7.0¢", 0.665: "66.5¢", 0.9995: "100.0¢", 1: "100.0¢",
	} {
		if got := Price(in); got != want {
			t.Errorf("Price(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestDelta(t *testing.T) {
	for in, want := range map[float64]string{
		0.035: "+3.5¢", -0.01: "-1.0¢", 0.5: "+50.0¢",
		// No change, and a change too small to show, carry no sign.
		0: "0.0¢", 0.0004: "0.0¢", -0.0004: "0.0¢",
	} {
		if got := Delta(in); got != want {
			t.Errorf("Delta(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestUntil(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	const day = 24 * time.Hour
	for in, want := range map[time.Duration]string{
		0:                "now",
		59 * time.Second: "now",
		40 * time.Minute: "40m",
		5 * time.Hour:    "5h",
		day - time.Hour:  "23h",
		12 * day:         "12d",
		29 * day:         "29d",
		95 * day:         "3mo",
		364 * day:        "12mo",
		800 * day:        "2y",
		// Gone by.
		-90 * time.Second: "-1m",
		-3 * day:          "-3d",
	} {
		if got := Until(now.Add(in), now); got != want {
			t.Errorf("Until(now%+v) = %q, want %q", in, got, want)
		}
	}
}
