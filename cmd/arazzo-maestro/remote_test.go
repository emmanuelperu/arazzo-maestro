package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
)

// remoteShopProject serves the shop OpenAPI contract from a local TLS
// server and writes a copy of shop.arazzo.yaml pointing at it. The
// server's certificate is trusted for the duration of the test.
func remoteShopProject(t *testing.T) string {
	t.Helper()
	spec, err := os.ReadFile(filepath.Join(examplesDir(t), "shop-openapi.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(spec)
	}))
	t.Cleanup(srv.Close)
	remoteTransport = srv.Client().Transport
	t.Cleanup(func() { remoteTransport = nil })

	arazzo, err := os.ReadFile(filepath.Join(examplesDir(t), "shop.arazzo.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	remote := strings.Replace(string(arazzo), "url: ./shop-openapi.yaml", "url: "+srv.URL+"/shop-openapi.yaml", 1)
	if remote == string(arazzo) {
		t.Fatal("shop.arazzo.yaml no longer references ./shop-openapi.yaml")
	}
	path := filepath.Join(t.TempDir(), "shop.arazzo.yaml")
	if err := os.WriteFile(path, []byte(remote), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLintRemoteSourceRequiresAllowRemote(t *testing.T) {
	path := remoteShopProject(t)
	out, _, err := runCmd(t, "lint", path)
	if err == nil {
		t.Fatalf("lint must refuse an HTTPS source without --allow-remote\n%s", out)
	}
	if !strings.Contains(out, "--allow-remote") {
		t.Errorf("finding should hint --allow-remote, got %q", out)
	}
}

func TestLintRemoteSourceWithAllowRemote(t *testing.T) {
	path := remoteShopProject(t)
	out, _, err := runCmd(t, "lint", path, "--allow-remote")
	if err != nil {
		t.Fatalf("lint should pass with --allow-remote, got %v\n%s", err, out)
	}
	if !strings.Contains(out, "no issues found") {
		t.Errorf("expected success message, got %q", out)
	}
}

func TestLintRemoteSourceLimitHint(t *testing.T) {
	path := remoteShopProject(t)
	out, _, err := runCmd(t, "lint", path, "--allow-remote", "--remote-max-size", "1KiB")
	if err == nil || !strings.Contains(out, "--remote-max-size") {
		t.Errorf("want a size-limit finding naming --remote-max-size, got %v\n%s", err, out)
	}
}

func TestTestGenRemoteSource(t *testing.T) {
	path := remoteShopProject(t)
	for _, kind := range []string{"e2e", "perf"} {
		t.Run(kind, func(t *testing.T) {
			if _, _, err := runCmd(t, "test", "gen", kind, path, "-o", t.TempDir()); err == nil || !strings.Contains(err.Error(), "--allow-remote") {
				t.Errorf("want an --allow-remote hint without the flag, got %v", err)
			}
			out, _, err := runCmd(t, "test", "gen", kind, path, "-o", t.TempDir(), "--allow-remote")
			if err != nil {
				t.Fatalf("generation with --allow-remote failed: %v", err)
			}
			if !strings.Contains(out, "wrote ") {
				t.Errorf("expected written files, got %q", out)
			}
		})
	}
}

func TestRemoteFlagsValidation(t *testing.T) {
	shop := filepath.Join(examplesDir(t), "shop.arazzo.yaml")
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "size without allow", args: []string{"--remote-max-size", "1MiB"}, wantErr: "--remote-max-size requires --allow-remote"},
		{name: "timeout without allow", args: []string{"--remote-timeout", "10s"}, wantErr: "--remote-timeout requires --allow-remote"},
		{name: "redirects without allow", args: []string{"--remote-max-redirects", "0"}, wantErr: "--remote-max-redirects requires --allow-remote"},
		{name: "zero size", args: []string{"--allow-remote", "--remote-max-size", "0"}, wantErr: "must be positive"},
		{name: "negative size", args: []string{"--allow-remote", "--remote-max-size", "-1KiB"}, wantErr: "must be positive"},
		{name: "unknown unit", args: []string{"--allow-remote", "--remote-max-size", "2MB"}, wantErr: "is not a size"},
		{name: "zero timeout", args: []string{"--allow-remote", "--remote-timeout", "0s"}, wantErr: "must be positive"},
		{name: "negative timeout", args: []string{"--allow-remote", "--remote-timeout", "-1s"}, wantErr: "must be positive"},
		{name: "negative redirects", args: []string{"--allow-remote", "--remote-max-redirects", "-1"}, wantErr: "must be zero or more"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, command := range [][]string{
				{"lint", shop},
				{"test", "gen", "e2e", shop, "-o", t.TempDir()},
				{"test", "gen", "perf", shop, "-o", t.TempDir()},
				{"test", "run", "e2e", shop, "--base-url", "http://localhost:1"},
			} {
				_, _, err := runCmd(t, append(command, tt.args...)...)
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("%s: want error containing %q, got %v", strings.Join(command[:2], " "), tt.wantErr, err)
				}
			}
		})
	}
}

func TestRemoteFlagsDefaultsLeaveLocalSourcesUntouched(t *testing.T) {
	shop := filepath.Join(examplesDir(t), "shop.arazzo.yaml")
	if _, _, err := runCmd(t, "lint", shop, "--allow-remote", "--remote-timeout", "1s"); err != nil {
		t.Errorf("remote flags must not affect local sources: %v", err)
	}
}

func TestParseByteSize(t *testing.T) {
	tests := []struct {
		raw     string
		want    int64
		wantErr string
	}{
		{raw: "1024", want: 1024},
		{raw: "512KiB", want: 512 << 10},
		{raw: defaultRemoteMaxSize, want: oasresolver.DefaultRemoteMaxSize},
		{raw: " 3MiB ", want: 3 << 20},
		{raw: "MiB", wantErr: "is not a size"},
		{raw: "9223372036854775807", wantErr: "is too large"},
		{raw: "9000000000000MiB", wantErr: "is too large"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := parseByteSize(tt.raw)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Errorf("got (%d, %v), want %d", got, err, tt.want)
			}
		})
	}
}
