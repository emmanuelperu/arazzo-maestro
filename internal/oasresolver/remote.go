package oasresolver

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Default bounds for fetching an HTTPS source.
const (
	DefaultRemoteMaxSize      int64 = 2 << 20
	DefaultRemoteTimeout            = 5 * time.Second
	DefaultRemoteMaxRedirects       = 2
)

// Sentinel errors returned (wrapped) when a remote source is refused.
var (
	ErrRemoteDisabled   = errors.New("remote source URLs are disabled")
	ErrPlainHTTP        = errors.New("plain HTTP source URLs are not supported")
	ErrTooLarge         = errors.New("remote source exceeds the size limit")
	ErrTimeout          = errors.New("remote source fetch timed out")
	ErrTooManyRedirects = errors.New("remote source exceeds the redirect limit")
)

// RemoteOptions controls whether and how HTTPS sources are fetched.
// Remote loading stays off unless Allow is set.
type RemoteOptions struct {
	Allow        bool
	MaxSize      int64
	Timeout      time.Duration
	MaxRedirects int
	// Transport overrides the HTTP transport; nil uses http.DefaultTransport.
	Transport http.RoundTripper
}

// DefaultRemoteOptions returns remote loading disabled, with the default
// bounds ready for when it is enabled.
func DefaultRemoteOptions() RemoteOptions {
	return RemoteOptions{
		MaxSize:      DefaultRemoteMaxSize,
		Timeout:      DefaultRemoteTimeout,
		MaxRedirects: DefaultRemoteMaxRedirects,
	}
}

// remoteURL reports whether rawURL is an http(s) URL, returning it parsed.
func remoteURL(rawURL string) (*url.URL, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, false
	}
	scheme := strings.ToLower(u.Scheme)
	return u, scheme == "http" || scheme == "https"
}

// fetch downloads an HTTPS source within the bounds of opts.
func fetch(u *url.URL, opts RemoteOptions) ([]byte, error) {
	rawURL := u.Redacted()
	if strings.ToLower(u.Scheme) != "https" {
		return nil, plainHTTPError(rawURL)
	}
	if !opts.Allow {
		return nil, fmt.Errorf("%w\n\turl: %s\n\thint: pass --allow-remote to fetch HTTPS sources, or use a local file", ErrRemoteDisabled, rawURL)
	}
	client := &http.Client{
		Transport: opts.Transport,
		Timeout:   opts.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.URL.Scheme != "https" {
				return plainHTTPError(req.URL.Redacted())
			}
			if len(via) > opts.MaxRedirects {
				return fmt.Errorf("%w (%d)\n\thint: raise --remote-max-redirects", ErrTooManyRedirects, opts.MaxRedirects)
			}
			return nil
		},
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, fetchError(rawURL, err, opts)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", rawURL, resp.Status)
	}
	if resp.ContentLength > opts.MaxSize {
		return nil, tooLargeError(rawURL, opts)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, opts.MaxSize+1))
	if err != nil {
		return nil, fetchError(rawURL, err, opts)
	}
	if int64(len(raw)) > opts.MaxSize {
		return nil, tooLargeError(rawURL, opts)
	}
	return raw, nil
}

func fetchError(rawURL string, err error, opts RemoteOptions) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("%w after %s\n\turl: %s\n\thint: raise --remote-timeout", ErrTimeout, opts.Timeout, rawURL)
	}
	return fmt.Errorf("fetch %s: %w", rawURL, err)
}

func plainHTTPError(rawURL string) error {
	return fmt.Errorf("%w\n\turl: %s\n\thint: serve the contract over HTTPS, or use a local file", ErrPlainHTTP, rawURL)
}

func tooLargeError(rawURL string, opts RemoteOptions) error {
	return fmt.Errorf("%w (%d bytes)\n\turl: %s\n\thint: raise --remote-max-size", ErrTooLarge, opts.MaxSize, rawURL)
}
