//go:build ignore

// Command record re-records the API responses in testdata from the live
// Polymarket services. Run it with `make fixtures`.
//
// The responses are stored as the services sent them, with two changes that
// keep the files reviewable: they are re-indented, and every list of markets
// inside an event is cut to its first few. Object keys come out sorted.
//
// What is trading changes from day to day, so a new recording holds different
// events from the last one. The tests that read these files therefore check
// shape (an ID is present, a price is between 0 and 1) rather than values.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

const (
	gamma = "https://gamma-api.polymarket.com"
	clob  = "https://clob.polymarket.com"
	data  = "https://data-api.polymarket.com/v2"

	// marketsPerEvent is how many of an event's markets are kept.
	marketsPerEvent = 3
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "record:", err)
		os.Exit(1)
	}
}

func run() error {
	listing := url.Values{
		"limit":     {"3"},
		"order":     {"volume24hr"},
		"ascending": {"false"},
		"closed":    {"false"},
	}

	events, err := record("events.json", http.StatusOK, gamma+"/events/keyset", listing)
	if err != nil {
		return err
	}
	// The detail fixtures follow one market that is actually trading, so
	// that it has a book, a history and trades to record.
	eventID, market, err := tradingMarket(events)
	if err != nil {
		return err
	}

	listing.Set("include_tag", "true")
	if _, err := record("markets.json", http.StatusOK, gamma+"/markets/keyset", listing); err != nil {
		return err
	}

	now := time.Now()
	calls := []struct {
		file   string
		status int
		url    string
		query  url.Values
	}{
		{"event.json", http.StatusOK, gamma + "/events/" + eventID, nil},
		{"market.json", http.StatusOK, gamma + "/markets/" + market.id, nil},
		{"tag.json", http.StatusOK, gamma + "/tags/slug/politics", nil},
		{"related_tags.json", http.StatusOK, gamma + "/tags/slug/politics/related-tags/tags", url.Values{
			"status":     {"active"},
			"omit_empty": {"true"},
		}},
		{"search.json", http.StatusOK, gamma + "/public-search", url.Values{
			"q":              {"election"},
			"limit_per_type": {"2"},
			"events_status":  {"active"},
		}},
		{"book.json", http.StatusOK, clob + "/book", url.Values{"token_id": {market.tokenID}}},
		// A limit below the number of points, so that the page has a cursor.
		{"prices_history.json", http.StatusOK, data + "/prices-history", url.Values{
			"token_id":       {market.tokenID},
			"interval":       {"1d"},
			"bucket_seconds": {"3600"},
			"limit":          {"12"},
		}},
		{"trades.json", http.StatusOK, data + "/trades", url.Values{
			"condition": {market.conditionID},
			"limit":     {"5"},
		}},

		// The errors the client has to report well.
		{"error_keyset_offset.json", http.StatusUnprocessableEntity, gamma + "/events/keyset", url.Values{
			"limit":  {"2"},
			"offset": {"5"},
		}},
		{"error_tag_not_found.json", http.StatusNotFound, gamma + "/tags/slug/no-such-tag-xyzzy", nil},
		{"error_history_range.json", http.StatusBadRequest, data + "/prices-history", url.Values{
			"token_id": {market.tokenID},
			"start":    {fmt.Sprint(now.AddDate(0, 0, -60).Unix())},
			"end":      {fmt.Sprint(now.Unix())},
		}},
	}
	for _, c := range calls {
		if _, err := record(c.file, c.status, c.url, c.query); err != nil {
			return err
		}
	}
	return nil
}

// record fetches a URL, checks it answered with the status the fixture is
// meant to capture, and writes the tidied body to testdata/file.
func record(file string, wantStatus int, u string, query url.Values) (any, error) {
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "polymarket-tui/fixtures")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != wantStatus {
		return nil, fmt.Errorf("%s: status %d, want %d: %.200s", u, resp.StatusCode, wantStatus, body)
	}

	// UseNumber keeps every number exactly as it was written.
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("%s: %w", u, err)
	}
	v = trim(v)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	path := filepath.Join("testdata", file)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return nil, err
	}
	fmt.Printf("%-28s %d  %d bytes\n", file, resp.StatusCode, buf.Len())
	return v, nil
}

// trim cuts every "markets" list down to marketsPerEvent, however deep it is.
// An event can hold dozens of markets, each several kilobytes.
func trim(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for key, child := range v {
			if list, ok := child.([]any); ok && key == "markets" && len(list) > marketsPerEvent {
				child = list[:marketsPerEvent]
			}
			v[key] = trim(child)
		}
	case []any:
		for i, child := range v {
			v[i] = trim(child)
		}
	}
	return v
}

type marketRef struct {
	id, conditionID, tokenID string
}

// tradingMarket picks, from an events listing, the first market that is
// accepting orders and traded in the last day, and returns it with the ID of
// its event.
func tradingMarket(listing any) (eventID string, m marketRef, err error) {
	root, _ := listing.(map[string]any)
	events, _ := root["events"].([]any)
	for _, e := range events {
		event, _ := e.(map[string]any)
		markets, _ := event["markets"].([]any)
		for _, raw := range markets {
			market, _ := raw.(map[string]any)
			if accepting, _ := market["acceptingOrders"].(bool); !accepting {
				continue
			}
			// Accepting orders is not enough: a market nobody has traded
			// yet has no trades to record.
			volume, _ := market["volume24hr"].(json.Number)
			if v, err := volume.Float64(); err != nil || v <= 0 {
				continue
			}
			// The token IDs are a JSON array inside a string.
			var tokens []string
			encoded, _ := market["clobTokenIds"].(string)
			if json.Unmarshal([]byte(encoded), &tokens) != nil || len(tokens) == 0 {
				continue
			}
			eventID, _ = event["id"].(string)
			m.id, _ = market["id"].(string)
			m.conditionID, _ = market["conditionId"].(string)
			m.tokenID = tokens[0]
			return eventID, m, nil
		}
	}
	return "", m, errors.New("no market trading among the top events")
}
