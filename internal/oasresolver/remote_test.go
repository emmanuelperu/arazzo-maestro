package oasresolver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

// newSpecServer serves basicSpec over TLS from a local httptest server.
// Every remote test talks to such a server: CI never reaches a real host.
func newSpecServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	if handler == nil {
		handler = func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, basicSpec)
		}
	}
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// trustedRemote returns remote options allowing HTTPS and trusting the
// test server's self-signed certificate.
func trustedRemote(srv *httptest.Server) RemoteOptions {
	opts := DefaultRemoteOptions()
	opts.Allow = true
	opts.Transport = srv.Client().Transport
	return opts
}

func loadOne(t *testing.T, url string, remote RemoteOptions) SourceResult {
	t.Helper()
	results := LoadAll([]model.SourceDescription{{Name: "api", Type: "openapi", URL: url}}, t.TempDir(), remote)
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	return results[0]
}

func wantErr(t *testing.T, r SourceResult, target error, contains ...string) {
	t.Helper()
	if r.Err == nil {
		t.Fatalf("want error, got source %+v", r)
	}
	if target != nil && !errors.Is(r.Err, target) {
		t.Errorf("want errors.Is(%v), got %v", target, r.Err)
	}
	for _, s := range contains {
		if !strings.Contains(r.Err.Error(), s) {
			t.Errorf("error %q does not contain %q", r.Err, s)
		}
	}
}

func TestLoadAllHTTPSDisabledByDefault(t *testing.T) {
	var requests atomic.Int32
	srv := newSpecServer(t, func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) })
	r := loadOne(t, srv.URL+"/spec.yaml", DefaultRemoteOptions())
	wantErr(t, r, ErrRemoteDisabled, "--allow-remote")
	if n := requests.Load(); n != 0 {
		t.Errorf("no request may be sent without --allow-remote, got %d", n)
	}
}

func TestLoadAllHTTPSAllowed(t *testing.T) {
	srv := newSpecServer(t, nil)
	url := srv.URL + "/spec.yaml"
	r := loadOne(t, url, trustedRemote(srv))
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Path != url {
		t.Errorf("remote path should be the URL, got %q", r.Path)
	}
	op, err := r.Source.Resolve("getUser")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if op.Method != "GET" || op.Path != "/users/{id}" {
		t.Errorf("unexpected operation: %+v", op)
	}
}

func TestLoadAllHTTPRejectedEvenWhenAllowed(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(srv.Close)
	remote := DefaultRemoteOptions()
	remote.Allow = true
	r := loadOne(t, srv.URL+"/spec.yaml", remote)
	wantErr(t, r, ErrPlainHTTP, "HTTPS")
	if n := requests.Load(); n != 0 {
		t.Errorf("plain HTTP must never be requested, got %d", n)
	}
}

func TestLoadAllHTTPSNon2xx(t *testing.T) {
	srv := newSpecServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	url := srv.URL + "/spec.yaml"
	r := loadOne(t, url, trustedRemote(srv))
	wantErr(t, r, nil, "404", url)
}

func TestLoadAllHTTPSSizeCap(t *testing.T) {
	big := basicSpec + "# " + strings.Repeat("x", 4096) + "\n"
	tests := []struct {
		name          string
		contentLength bool
	}{
		{name: "declared content length", contentLength: true},
		{name: "streamed body", contentLength: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newSpecServer(t, func(w http.ResponseWriter, _ *http.Request) {
				if tt.contentLength {
					w.Header().Set("Content-Length", strconv.Itoa(len(big)))
				} else {
					w.(http.Flusher).Flush()
				}
				_, _ = fmt.Fprint(w, big)
			})
			remote := trustedRemote(srv)
			remote.MaxSize = 1024
			wantErr(t, loadOne(t, srv.URL, remote), ErrTooLarge, "--remote-max-size")

			remote.MaxSize = int64(len(big))
			if r := loadOne(t, srv.URL, remote); r.Err != nil {
				t.Errorf("raised cap should accept the body: %v", r.Err)
			}
		})
	}
}

func TestLoadAllHTTPSTimeout(t *testing.T) {
	srv := newSpecServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_, _ = fmt.Fprint(w, basicSpec)
	})
	remote := trustedRemote(srv)
	remote.Timeout = 20 * time.Millisecond
	wantErr(t, loadOne(t, srv.URL, remote), ErrTimeout, "--remote-timeout")

	remote.Timeout = 5 * time.Second
	if r := loadOne(t, srv.URL, remote); r.Err != nil {
		t.Errorf("raised timeout should succeed: %v", r.Err)
	}
}

// redirectServer redirects /hop/<n> to /hop/<n-1> and serves the spec at
// /hop/0, so /hop/<n> costs exactly n redirects.
func redirectServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newSpecServer(t, func(w http.ResponseWriter, r *http.Request) {
		var n int
		if _, err := fmt.Sscanf(r.URL.Path, "/hop/%d", &n); err != nil || n == 0 {
			_, _ = fmt.Fprint(w, basicSpec)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/hop/%d", n-1), http.StatusFound)
	})
}

func TestLoadAllHTTPSRedirects(t *testing.T) {
	srv := redirectServer(t)
	tests := []struct {
		name         string
		hops         int
		maxRedirects int
		wantErr      bool
	}{
		{name: "default follows 2", hops: 2, maxRedirects: DefaultRemoteMaxRedirects},
		{name: "default refuses 3", hops: 3, maxRedirects: DefaultRemoteMaxRedirects, wantErr: true},
		{name: "raised cap follows 3", hops: 3, maxRedirects: 3},
		{name: "zero refuses first", hops: 1, maxRedirects: 0, wantErr: true},
		{name: "zero still loads direct", hops: 0, maxRedirects: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := trustedRemote(srv)
			remote.MaxRedirects = tt.maxRedirects
			r := loadOne(t, fmt.Sprintf("%s/hop/%d", srv.URL, tt.hops), remote)
			if tt.wantErr {
				wantErr(t, r, ErrTooManyRedirects, "--remote-max-redirects")
				return
			}
			if r.Err != nil {
				t.Errorf("unexpected error: %v", r.Err)
			}
		})
	}
}

func TestLoadAllHTTPSRedirectToHTTPRefused(t *testing.T) {
	var requests atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests.Add(1) }))
	t.Cleanup(plain.Close)
	srv := newSpecServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/spec.yaml", http.StatusFound)
	})
	wantErr(t, loadOne(t, srv.URL, trustedRemote(srv)), ErrPlainHTTP)
	if n := requests.Load(); n != 0 {
		t.Errorf("redirect target over plain HTTP must not be requested, got %d", n)
	}
}

func TestLoadAllHTTPSUntrustedCertificate(t *testing.T) {
	srv := newSpecServer(t, nil)
	remote := DefaultRemoteOptions()
	remote.Allow = true
	r := loadOne(t, srv.URL, remote)
	wantErr(t, r, nil, "certificate")
}

func TestLoadAllHTTPSInvalidSpec(t *testing.T) {
	srv := newSpecServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "not: [an openapi")
	})
	wantErr(t, loadOne(t, srv.URL, trustedRemote(srv)), nil, srv.URL)
}

func TestLoadAllHTTPSTruncatedBody(t *testing.T) {
	srv := newSpecServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(basicSpec)+100))
		_, _ = fmt.Fprint(w, basicSpec)
	})
	wantErr(t, loadOne(t, srv.URL, trustedRemote(srv)), nil, "fetch "+srv.URL)
}

func TestLoadAllRedactsURLCredentials(t *testing.T) {
	r := loadOne(t, "https://user:s3cret@example.com/spec.yaml", DefaultRemoteOptions())
	wantErr(t, r, ErrRemoteDisabled)
	if strings.Contains(r.Err.Error(), "s3cret") {
		t.Errorf("error leaks the URL password: %v", r.Err)
	}
}
