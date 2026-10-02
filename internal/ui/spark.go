package ui

import (
	"math"
	"strings"

	"github.com/farrellm/polymarket/internal/api"
)

// sparkBlocks fill a cell from the bottom, by eighths.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// resample spreads a price history over a number of columns of equal time,
// oldest first. A column takes the last price at or before its end, so a
// stretch with no points repeats the price it began with, which is what the
// price was. A history with no price in it is nil.
func resample(points []api.PricePoint, columns int) []float64 {
	var priced []api.PricePoint
	for _, p := range points {
		if p.Price.Valid && !p.Time.IsZero() {
			priced = append(priced, p)
		}
	}
	if len(priced) == 0 || columns <= 0 {
		return nil
	}

	start := priced[0].Time.Unix()
	span := priced[len(priced)-1].Time.Unix() - start
	out := make([]float64, columns)
	at := 0
	for i := range out {
		end := start + span*int64(i+1)/int64(columns)
		for at+1 < len(priced) && priced[at+1].Time.Unix() <= end {
			at++
		}
		out[i] = priced[at].Price.Value
	}
	return out
}

// bounds are the least and the greatest of the values.
func bounds(values []float64) (low, high float64) {
	low, high = math.Inf(1), math.Inf(-1)
	for _, v := range values {
		low, high = min(low, v), max(high, v)
	}
	return low, high
}

// spark draws values as columns of blocks, one column each and rows lines
// tall, from the least of them at the bottom to the greatest at the top. The
// least still shows an eighth of a cell, so the line is never broken, and
// values that are all the same run across the middle.
func spark(values []float64, rows int) []string {
	if len(values) == 0 || rows <= 0 {
		return nil
	}
	const eighths = 8
	steps := rows * eighths
	low, high := bounds(values)

	heights := make([]int, len(values))
	for i, v := range values {
		heights[i] = steps / 2
		if high > low {
			heights[i] = 1 + int(math.Round((v-low)/(high-low)*float64(steps-1)))
		}
	}

	lines := make([]string, rows)
	for r := range rows {
		// The eighths that lie under this row belong to the rows below it.
		under := (rows - 1 - r) * eighths
		var b strings.Builder
		for _, h := range heights {
			switch fill := h - under; {
			case fill <= 0:
				b.WriteByte(' ')
			case fill >= eighths:
				b.WriteRune(sparkBlocks[eighths-1])
			default:
				b.WriteRune(sparkBlocks[fill-1])
			}
		}
		lines[r] = b.String()
	}
	return lines
}
