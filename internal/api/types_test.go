package api

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFloat(t *testing.T) {
	tests := []struct {
		in    string
		want  float64
		valid bool
	}{
		{`1509041.72`, 1509041.72, true},
		{`"1509041.72"`, 1509041.72, true},
		{`0`, 0, true},
		{`"0"`, 0, true},
		{`-0.3645`, -0.3645, true},
		{`1e3`, 1000, true},
		{`null`, 0, false},
		{`""`, 0, false},
		{`"n/a"`, 0, false},
	}
	for _, tt := range tests {
		var got Float
		if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
			t.Errorf("%s: %v", tt.in, err)
			continue
		}
		if got.Value != tt.want || got.Valid != tt.valid {
			t.Errorf("%s = %+v, want {%v %v}", tt.in, got, tt.want, tt.valid)
		}
	}
}

func TestFloatAbsentIsNotValid(t *testing.T) {
	var m Market
	if err := json.Unmarshal([]byte(`{"id":"1","bestBid":0}`), &m); err != nil {
		t.Fatal(err)
	}
	if !m.BestBid.Valid {
		t.Error("bestBid: a zero that was sent must be valid")
	}
	if m.BestAsk.Valid {
		t.Error("bestAsk: a value that was not sent must not be valid")
	}
}

func TestFloatRejectsOtherJSON(t *testing.T) {
	for _, in := range []string{`{}`, `[1]`, `true`} {
		var f Float
		if err := json.Unmarshal([]byte(in), &f); err == nil {
			t.Errorf("%s decoded as a number, want an error", in)
		}
	}
}

func TestTime(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{`"2026-10-29T03:59:00Z"`, time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC)},
		{`"2026-06-17T23:34:28.470106Z"`, time.Date(2026, 6, 17, 23, 34, 28, 470106000, time.UTC)},
		{`"2026-10-29T05:59:00+02:00"`, time.Date(2026, 10, 29, 3, 59, 0, 0, time.UTC)},
		{`"2026-10-29"`, time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)},
		{`"2023-10-25 18:55:50.674+00"`, time.Date(2023, 10, 25, 18, 55, 50, 674000000, time.UTC)},
		// The Data API's timestamps are seconds, as numbers.
		{`1790947065`, time.Date(2026, 10, 2, 13, 17, 45, 0, time.UTC)},
		// The CLOB's is milliseconds, in a string.
		{`"1790947394561"`, time.Date(2026, 10, 2, 13, 23, 14, 561000000, time.UTC)},
		{`null`, time.Time{}},
		{`""`, time.Time{}},
		{`"soon"`, time.Time{}},
	}
	for _, tt := range tests {
		var got Time
		if err := json.Unmarshal([]byte(tt.in), &got); err != nil {
			t.Errorf("%s: %v", tt.in, err)
			continue
		}
		if !got.Equal(tt.want) {
			t.Errorf("%s = %v, want %v", tt.in, got.Time, tt.want)
		}
		if !got.IsZero() && got.Location() != time.UTC {
			t.Errorf("%s: location %v, want UTC", tt.in, got.Location())
		}
	}
}

// token is a real token ID: 77 digits, which no integer type holds.
const token = "17010377994663817312158123655937348199252960045746746128731746451645055725586"

func TestMarketStringEncodedArrays(t *testing.T) {
	in := `{
		"id": "2589810",
		"outcomes": "[\"Yes\", \"No\"]",
		"outcomePrices": "[\"0.0025\", \"0.9975\"]",
		"clobTokenIds": "[\"` + token + `\", \"61725\"]",
		"volume": "3646658.32",
		"volumeNum": 1,
		"liquidity": "802699.68533",
		"volume24hr": 416742.55,
		"endDate": "2026-10-29T03:59:00Z",
		"acceptingOrders": true
	}`
	var m Market
	if err := json.Unmarshal([]byte(in), &m); err != nil {
		t.Fatal(err)
	}

	want := []Outcome{
		{Label: "Yes", Price: Float{0.0025, true}, TokenID: token},
		{Label: "No", Price: Float{0.9975, true}, TokenID: "61725"},
	}
	if len(m.Outcomes) != len(want) {
		t.Fatalf("outcomes = %+v, want %+v", m.Outcomes, want)
	}
	for i := range want {
		if m.Outcomes[i] != want[i] {
			t.Errorf("outcome %d = %+v, want %+v", i, m.Outcomes[i], want[i])
		}
	}

	// The string wins over volumeNum when both are sent.
	if m.Volume != (Float{3646658.32, true}) {
		t.Errorf("volume = %+v", m.Volume)
	}
	if m.Liquidity != (Float{802699.68533, true}) {
		t.Errorf("liquidity = %+v", m.Liquidity)
	}
	if m.Volume24h != (Float{416742.55, true}) {
		t.Errorf("volume24hr = %+v", m.Volume24h)
	}
	if m.ID != "2589810" || !m.AcceptingOrders || m.EndDate.IsZero() {
		t.Errorf("plain fields were not decoded: %+v", m)
	}
}

func TestMarketOutcomeForms(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []Outcome
	}{
		{
			name: "real arrays, with prices as numbers",
			in:   `{"outcomes":["Yes","No"],"outcomePrices":[0.25,0.75],"clobTokenIds":["1","2"]}`,
			want: []Outcome{
				{"Yes", Float{0.25, true}, "1"},
				{"No", Float{0.75, true}, "2"},
			},
		},
		{
			name: "more than two outcomes",
			in:   `{"outcomes":"[\"A\",\"B\",\"C\"]","outcomePrices":"[\"0.2\",\"0.3\",\"0.5\"]","clobTokenIds":"[\"1\",\"2\",\"3\"]"}`,
			want: []Outcome{
				{"A", Float{0.2, true}, "1"},
				{"B", Float{0.3, true}, "2"},
				{"C", Float{0.5, true}, "3"},
			},
		},
		{
			name: "no prices and no tokens",
			in:   `{"outcomes":"[\"Yes\",\"No\"]"}`,
			want: []Outcome{{Label: "Yes"}, {Label: "No"}},
		},
		{
			name: "empty strings and nulls mean no outcomes",
			in:   `{"outcomes":"","outcomePrices":null}`,
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m Market
			if err := json.Unmarshal([]byte(tt.in), &m); err != nil {
				t.Fatal(err)
			}
			if len(m.Outcomes) != len(tt.want) {
				t.Fatalf("outcomes = %+v, want %+v", m.Outcomes, tt.want)
			}
			for i := range tt.want {
				if m.Outcomes[i] != tt.want[i] {
					t.Errorf("outcome %d = %+v, want %+v", i, m.Outcomes[i], tt.want[i])
				}
			}
		})
	}
}

func TestMarketVolumeFallsBackToNum(t *testing.T) {
	var m Market
	if err := json.Unmarshal([]byte(`{"volumeNum": 12.5, "liquidityNum": 3}`), &m); err != nil {
		t.Fatal(err)
	}
	if m.Volume != (Float{12.5, true}) || m.Liquidity != (Float{3, true}) {
		t.Errorf("volume = %+v, liquidity = %+v", m.Volume, m.Liquidity)
	}
}

func TestMarketBadOutcomesIsAnError(t *testing.T) {
	var m Market
	if err := json.Unmarshal([]byte(`{"id":"7","outcomes":"not json"}`), &m); err == nil {
		t.Error("a market with unreadable outcomes decoded, want an error")
	}
}

// A decoded market is reused across refreshes, so decoding into one that
// already holds outcomes must replace them.
func TestMarketDecodeReplacesOutcomes(t *testing.T) {
	m := Market{Outcomes: []Outcome{{Label: "stale"}}}
	if err := json.Unmarshal([]byte(`{"outcomes":"[\"Yes\"]"}`), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Outcomes) != 1 || m.Outcomes[0].Label != "Yes" {
		t.Errorf("outcomes = %+v", m.Outcomes)
	}
}
