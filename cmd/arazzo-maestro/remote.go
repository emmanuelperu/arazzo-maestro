package main

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

// remoteTransport overrides the HTTPS transport used to fetch sources;
// nil keeps the default one.
var remoteTransport http.RoundTripper

const defaultRemoteMaxSize = "2MiB"

type remoteFlags struct {
	allow        bool
	maxSize      string
	timeout      time.Duration
	maxRedirects int
}

func addRemoteFlags(cmd *cobra.Command, f *remoteFlags) {
	cmd.Flags().BoolVar(&f.allow, "allow-remote", false, "Fetch HTTPS sourceDescriptions URLs (plain HTTP is always refused)")
	cmd.Flags().StringVar(&f.maxSize, "remote-max-size", defaultRemoteMaxSize, "Maximum size of a fetched source, in bytes or with a KiB/MiB suffix")
	cmd.Flags().DurationVar(&f.timeout, "remote-timeout", oasresolver.DefaultRemoteTimeout, "Timeout for fetching one source (e.g. 15s)")
	cmd.Flags().IntVar(&f.maxRedirects, "remote-max-redirects", oasresolver.DefaultRemoteMaxRedirects, "Redirects followed when fetching a source (0 follows none)")
}

// options validates the remote flags of cmd and turns them into
// oasresolver options. Tuning flags without --allow-remote are an error
// rather than a silent no-op.
func (f *remoteFlags) options(cmd *cobra.Command) (oasresolver.RemoteOptions, error) {
	if !f.allow {
		for _, name := range []string{"remote-max-size", "remote-timeout", "remote-max-redirects"} {
			if cmd.Flags().Changed(name) {
				return oasresolver.RemoteOptions{}, fmt.Errorf("--%s requires --allow-remote", name)
			}
		}
	}
	maxSize, err := parseByteSize(f.maxSize)
	if err != nil {
		return oasresolver.RemoteOptions{}, fmt.Errorf("invalid --remote-max-size: %w", err)
	}
	if f.timeout <= 0 {
		return oasresolver.RemoteOptions{}, fmt.Errorf("invalid --remote-timeout %s: must be positive", f.timeout)
	}
	if f.maxRedirects < 0 {
		return oasresolver.RemoteOptions{}, fmt.Errorf("invalid --remote-max-redirects %d: must be zero or more", f.maxRedirects)
	}
	return oasresolver.RemoteOptions{
		Allow:        f.allow,
		MaxSize:      maxSize,
		Timeout:      f.timeout,
		MaxRedirects: f.maxRedirects,
		Transport:    remoteTransport,
	}, nil
}

// parseByteSize reads a strictly positive size such as "2097152",
// "512KiB" or "2MiB".
func parseByteSize(raw string) (int64, error) {
	s := strings.TrimSpace(raw)
	shift := 0
	switch {
	case strings.HasSuffix(s, "MiB"):
		s, shift = strings.TrimSuffix(s, "MiB"), 20
	case strings.HasSuffix(s, "KiB"):
		s, shift = strings.TrimSuffix(s, "KiB"), 10
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a size (use bytes, KiB or MiB)", raw)
	}
	if n <= 0 {
		return 0, fmt.Errorf("%q must be positive", raw)
	}
	// Keep headroom for the one extra byte read to detect an oversized body.
	if n > (math.MaxInt64-1)>>shift {
		return 0, fmt.Errorf("%q is too large", raw)
	}
	return n << shift, nil
}
