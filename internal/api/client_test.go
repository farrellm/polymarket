package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestHeaders(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusOK, fixture(t, "tag.json"), func(r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "polymarket-tui/dev" {
			t.Errorf("User-Agent = %q", got)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
	}))
	if _, err := c.Tag(context.Background(), "politics"); err != nil {
		t.Fatal(err)
	}
}

func TestOptions(t *testing.T) {
	c := New(Options{})
	if c.gamma != DefaultGammaURL || c.clob != DefaultCLOBURL || c.data != DefaultDataURL {
		t.Errorf("default URLs = %q %q %q", c.gamma, c.clob, c.data)
	}
	if c.timeout != 15*time.Second {
		t.Errorf("default timeout = %v", c.timeout)
	}

	c = New(Options{GammaURL: "http://localhost:1/", UserAgent: "polymarket-tui/1.2.3", Timeout: time.Second})
	if c.gamma != "http://localhost:1" {
		t.Errorf("gamma URL = %q, want the trailing slash dropped", c.gamma)
	}
	if c.userAgent != "polymarket-tui/1.2.3" || c.timeout != time.Second {
		t.Errorf("user agent = %q, timeout = %v", c.userAgent, c.timeout)
	}
}

// An offset on a keyset endpoint is answered with 422: the client reports it
// with the service's own words, and does not try again.
func TestErrorResponse(t *testing.T) {
	var calls atomic.Int32
	body := fixture(t, "error_keyset_offset.json")
	c := newTestClient(t, serve(t, http.StatusUnprocessableEntity, body, func(*http.Request) {
		calls.Add(1)
	}))

	_, _, err := c.Events(context.Background(), EventsQuery{})
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want an *Error", err)
	}
	if apiErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d", apiErr.Status)
	}
	if apiErr.Body != string(body) {
		t.Errorf("body = %q", apiErr.Body)
	}
	if got, want := apiErr.Message(), "offset is not allowed on keyset endpoints"; got != want {
		t.Errorf("message = %q, want %q", got, want)
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1: a 422 is not retried", calls.Load())
	}
	if IsNotFound(err) {
		t.Error("IsNotFound is true for a 422")
	}
}

func TestErrorMessage(t *testing.T) {
	tests := []struct {
		body string
		want string
	}{
		{`{"error":"slug not found","type":"not found error"}`, "polymarket api: /x: 404 Not Found: slug not found"},
		{"<html>blocked</html>\n", "polymarket api: /x: 404 Not Found: <html>blocked</html>"},
		{"", "polymarket api: /x: 404 Not Found"},
	}
	for _, tt := range tests {
		err := &Error{Status: http.StatusNotFound, Body: tt.body, URL: "/x"}
		if got := err.Error(); got != tt.want {
			t.Errorf("body %q: error = %q, want %q", tt.body, got, tt.want)
		}
	}
}

func TestNotFound(t *testing.T) {
	c := newTestClient(t, serve(t, http.StatusNotFound, fixture(t, "error_tag_not_found.json"), nil))
	_, err := c.Tag(context.Background(), "no-such-tag")
	if !IsNotFound(err) {
		t.Errorf("error = %v, want one IsNotFound recognises", err)
	}
}

func TestRetryAfterIsHonoured(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "2")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write(fixture(t, "tag.json"))
	}))

	tag, err := c.Tag(context.Background(), "politics")
	if err != nil {
		t.Fatal(err)
	}
	if tag.Slug != "politics" {
		t.Errorf("tag = %+v", tag)
	}
	if calls.Load() != 2 {
		t.Errorf("%d requests, want 2", calls.Load())
	}
	if got := c.slept(); len(got) != 1 || got[0] != 2*time.Second {
		t.Errorf("pauses = %v, want one of 2s", got)
	}
}

func TestServerErrorIsRetried(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Write(fixture(t, "tag.json"))
	}))

	if _, err := c.Tag(context.Background(), "politics"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Errorf("%d requests, want 3", calls.Load())
	}

	// Exponential, with jitter in the upper half of each step.
	got := c.slept()
	if len(got) != 2 {
		t.Fatalf("pauses = %v, want two", got)
	}
	for i, step := range []time.Duration{baseBackoff, 2 * baseBackoff} {
		if got[i] < step/2 || got[i] > step {
			t.Errorf("pause %d = %v, want between %v and %v", i, got[i], step/2, step)
		}
	}
}

func TestRetriesRunOut(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))

	_, err := c.Tag(context.Background(), "politics")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("error = %v, want a 503", err)
	}
	if calls.Load() != maxAttempts {
		t.Errorf("%d requests, want %d", calls.Load(), maxAttempts)
	}
}

func TestBackoff(t *testing.T) {
	if got := backoff(0, time.Hour); got != maxRetryAfter {
		t.Errorf("a Retry-After of an hour gives %v, want it capped at %v", got, maxRetryAfter)
	}
	if got := backoff(1, 3*time.Second); got != 3*time.Second {
		t.Errorf("a Retry-After of 3s gives %v", got)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("7"); got != 7*time.Second {
		t.Errorf("seconds: %v", got)
	}
	date := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(date); got < 50*time.Second || got > time.Minute {
		t.Errorf("date: %v, want about a minute", got)
	}
	for _, in := range []string{"", "soon", "-5", time.Unix(0, 0).UTC().Format(http.TimeFormat)} {
		if got := parseRetryAfter(in); got != 0 {
			t.Errorf("%q: %v, want 0", in, got)
		}
	}
}

func TestContextCancellation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
	}))
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-started
		cancel()
	}()

	_, err := c.Tag(ctx, "politics")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1: a cancelled call is not retried", calls.Load())
	}
	if got := c.slept(); len(got) != 0 {
		t.Errorf("paused %v after cancellation", got)
	}
}

func TestCancelledBeforeTheRequest(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		calls.Add(1)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Tag(ctx, "politics"); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if calls.Load() != 0 {
		t.Errorf("%d requests, want none", calls.Load())
	}
}

// A request that outlives the timeout is given up on and tried again, and
// the caller's own context is untouched.
func TestTimeoutIsRetried(t *testing.T) {
	release := make(chan struct{})
	var calls atomic.Int32
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			<-release
			return
		}
		w.Write(fixture(t, "tag.json"))
	}))
	defer close(release)
	c.timeout = 50 * time.Millisecond

	if _, err := c.Tag(context.Background(), "politics"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("%d requests, want 2", calls.Load())
	}
}

func TestBadJSONIsNotRetried(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, serve(t, http.StatusOK, []byte(`{"id": `), func(*http.Request) {
		calls.Add(1)
	}))
	if _, err := c.Tag(context.Background(), "politics"); err == nil {
		t.Fatal("truncated JSON decoded, want an error")
	}
	if calls.Load() != 1 {
		t.Errorf("%d requests, want 1", calls.Load())
	}
}
