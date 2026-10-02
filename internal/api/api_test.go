package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
)

// fixture reads a recorded response from the repository's testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// testClient is a Client pointed at a test server, which never waits: its
// pauses between attempts are recorded instead of slept.
type testClient struct {
	*Client

	mu     sync.Mutex
	sleeps []time.Duration
}

func (c *testClient) slept() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.sleeps...)
}

func newTestClient(t *testing.T, h http.Handler) *testClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)

	tc := &testClient{Client: New(Options{
		GammaURL: srv.URL + "/gamma",
		CLOBURL:  srv.URL + "/clob",
		DataURL:  srv.URL + "/data/v2",
	})}
	tc.limiter = rate.NewLimiter(rate.Inf, 0)
	tc.sleep = func(_ context.Context, d time.Duration) error {
		tc.mu.Lock()
		defer tc.mu.Unlock()
		tc.sleeps = append(tc.sleeps, d)
		return nil
	}
	return tc
}

// serve answers every request with one recorded response, and hands the
// request to check first.
func serve(t *testing.T, status int, body []byte, check func(r *http.Request)) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if check != nil {
			check(r)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	})
}

// wantQuery checks the parameters a request carried: each key in want must
// have that value, and each key in absent must be missing.
func wantQuery(t *testing.T, r *http.Request, want map[string]string, absent ...string) {
	t.Helper()
	q := r.URL.Query()
	for key, value := range want {
		if got := q.Get(key); got != value {
			t.Errorf("%s: query %s = %q, want %q", r.URL.Path, key, got, value)
		}
	}
	for _, key := range absent {
		if q.Has(key) {
			t.Errorf("%s: query has %s=%q, want it left out", r.URL.Path, key, q.Get(key))
		}
	}
}
