// Package api is a client for Polymarket's public, read-only HTTP endpoints.
//
// Three services are involved: Gamma for discovery (events, markets, tags and
// search), the CLOB for the order book, and the Data API for price history and
// trades. None of them needs credentials.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// The services' public base URLs.
const (
	DefaultGammaURL = "https://gamma-api.polymarket.com"
	DefaultCLOBURL  = "https://clob.polymarket.com"
	DefaultDataURL  = "https://data-api.polymarket.com/v2"
)

const (
	defaultTimeout   = 15 * time.Second
	defaultUserAgent = "polymarket-tui/dev"

	// The documented limits are far higher, so this is politeness.
	requestsPerSecond = 10
	requestBurst      = 20

	maxAttempts = 3
	baseBackoff = 500 * time.Millisecond
	// A server asking for a longer pause than this is not worth waiting for
	// in an interactive program; the pause is cut short and the last attempt
	// reports whatever the server then says.
	maxRetryAfter = 30 * time.Second

	// An error body is kept for the message, not parsed as data.
	maxErrorBody = 64 << 10
)

// Options configures a Client. The zero value talks to the live services.
type Options struct {
	// Base URLs, without a trailing slash. Empty means the public service.
	GammaURL string
	CLOBURL  string
	DataURL  string

	// Timeout bounds one request, including reading its body. Zero means 15 s.
	Timeout time.Duration
	// UserAgent is sent with every request. Empty means polymarket-tui/dev.
	UserAgent string
	// HTTPClient makes the requests. Nil means a client of its own.
	HTTPClient *http.Client
}

// Client calls the Polymarket services. It is safe for concurrent use, and
// its rate limiter is shared by all three services.
type Client struct {
	gamma, clob, data string

	http      *http.Client
	limiter   *rate.Limiter
	timeout   time.Duration
	userAgent string

	// sleep waits between attempts. Tests replace it.
	sleep func(ctx context.Context, d time.Duration) error
}

// New builds a Client.
func New(o Options) *Client {
	c := &Client{
		gamma:     baseURL(o.GammaURL, DefaultGammaURL),
		clob:      baseURL(o.CLOBURL, DefaultCLOBURL),
		data:      baseURL(o.DataURL, DefaultDataURL),
		http:      o.HTTPClient,
		limiter:   rate.NewLimiter(requestsPerSecond, requestBurst),
		timeout:   o.Timeout,
		userAgent: o.UserAgent,
		sleep:     sleep,
	}
	if c.http == nil {
		c.http = &http.Client{}
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	if c.userAgent == "" {
		c.userAgent = defaultUserAgent
	}
	return c
}

func baseURL(given, fallback string) string {
	if given == "" {
		given = fallback
	}
	return strings.TrimRight(given, "/")
}

// Error is a response the service answered with a status other than 200.
type Error struct {
	// Status is the HTTP status code.
	Status int
	// Body is the response body, cut off if it is very long.
	Body string
	// URL is the request that failed.
	URL string
}

func (e *Error) Error() string {
	msg := e.Message()
	if msg == "" {
		return fmt.Sprintf("polymarket api: %s: %d %s", e.URL, e.Status, http.StatusText(e.Status))
	}
	return fmt.Sprintf("polymarket api: %s: %d %s: %s", e.URL, e.Status, http.StatusText(e.Status), msg)
}

// Message is the service's own explanation: the "error" member of a JSON
// body, or else the start of the body.
func (e *Error) Message() string {
	var body struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(e.Body), &body) == nil && body.Error != "" {
		return body.Error
	}
	const limit = 200
	msg := strings.TrimSpace(e.Body)
	if len(msg) > limit {
		msg = msg[:limit] + "…"
	}
	return msg
}

// retryable reports whether a later attempt could succeed.
func (e *Error) retryable() bool {
	return e.Status == http.StatusTooManyRequests || e.Status >= 500
}

// get requests base+path and decodes the JSON it answers with into out. It
// retries a 429, a 5xx or a failure to get an answer at all, up to
// maxAttempts times in total.
func (c *Client) get(ctx context.Context, base, path string, query url.Values, out any) error {
	u := base + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var (
		err  error
		wait time.Duration
	)
	for attempt := range maxAttempts {
		if attempt > 0 {
			if err := c.sleep(ctx, wait); err != nil {
				return err
			}
		}
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}

		var (
			retry      bool
			retryAfter time.Duration
		)
		retry, retryAfter, err = c.attempt(ctx, u, out)
		if err == nil {
			return nil
		}
		// The caller giving up is never worth another attempt, whatever
		// shape the failure took.
		if !retry || ctx.Err() != nil {
			return err
		}
		wait = backoff(attempt, retryAfter)
	}
	return err
}

// attempt makes one request. retry reports whether the failure is worth
// another attempt, and retryAfter is the pause the server asked for, if any.
func (c *Client) attempt(ctx context.Context, u string, out any) (retry bool, retryAfter time.Duration, err error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, http.NoBody)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		// No answer: a refused or dropped connection, or the timeout.
		return true, 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		apiErr := &Error{Status: resp.StatusCode, Body: string(body), URL: u}
		return apiErr.retryable(), parseRetryAfter(resp.Header.Get("Retry-After")), apiErr
	}

	if out == nil {
		return false, 0, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return false, 0, fmt.Errorf("polymarket api: %s: decoding the response: %w", u, err)
	}
	return false, 0, nil
}

// backoff is the pause before the attempt after the given one: what the
// server asked for if it said, else exponential with jitter.
func backoff(attempt int, retryAfter time.Duration) time.Duration {
	if retryAfter > 0 {
		return min(retryAfter, maxRetryAfter)
	}
	d := baseBackoff << attempt
	return d/2 + rand.N(d/2)
}

// parseRetryAfter reads a Retry-After header, which is either a number of
// seconds or an HTTP date. Anything else is no instruction at all.
func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		return max(0, time.Duration(secs)*time.Second)
	}
	if at, err := http.ParseTime(v); err == nil {
		return max(0, time.Until(at))
	}
	return 0
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// IsNotFound reports whether err is the service saying there is no such
// thing: an unknown tag slug, market ID or token.
func IsNotFound(err error) bool {
	var apiErr *Error
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
