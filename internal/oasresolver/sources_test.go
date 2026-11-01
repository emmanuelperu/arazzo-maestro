package oasresolver

import (
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

func TestLoadAllLoadsOpenAPISources(t *testing.T) {
	path := writeSpec(t, basicSpec)
	results := LoadAll([]model.SourceDescription{
		{Name: "api", Type: "openapi", URL: filepath.Base(path)},
	}, filepath.Dir(path), DefaultRemoteOptions())
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Err != nil {
		t.Fatalf("unexpected error: %v", r.Err)
	}
	if r.Name != "api" || r.Source == nil || r.Path != path {
		t.Errorf("unexpected result: %+v", r)
	}
}

func TestLoadAllSkipsNonOpenAPISources(t *testing.T) {
	results := LoadAll([]model.SourceDescription{
		{Name: "wf", Type: "arazzo", URL: "other.yaml"},
	}, t.TempDir(), DefaultRemoteOptions())
	if len(results) != 0 {
		t.Fatalf("want 0 results, got %d", len(results))
	}
}

func TestLoadAllReportsMissingFile(t *testing.T) {
	dir := t.TempDir()
	results := LoadAll([]model.SourceDescription{
		{Name: "api", URL: "missing.yaml"},
	}, dir, DefaultRemoteOptions())
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	r := results[0]
	if !errors.Is(r.Err, fs.ErrNotExist) {
		t.Errorf("want fs.ErrNotExist, got %v", r.Err)
	}
	if r.Path != filepath.Join(dir, "missing.yaml") {
		t.Errorf("resolved path should be set on load errors, got %q", r.Path)
	}
}

func TestLoadAllRejectsRemoteURLByDefault(t *testing.T) {
	results := LoadAll([]model.SourceDescription{
		{Name: "api", URL: "https://example.com/spec.yaml"},
	}, t.TempDir(), DefaultRemoteOptions())
	if len(results) != 1 || results[0].Err == nil {
		t.Fatalf("want 1 errored result, got %+v", results)
	}
	if !strings.Contains(results[0].Err.Error(), "--allow-remote") {
		t.Errorf("unexpected error: %v", results[0].Err)
	}
	if results[0].Path != "" {
		t.Errorf("path should be empty on resolution failure, got %q", results[0].Path)
	}
}

func TestLoadAllReportsInvalidURL(t *testing.T) {
	results := LoadAll([]model.SourceDescription{{Name: "api", URL: ":"}}, t.TempDir(), DefaultRemoteOptions())
	if len(results) != 1 || results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "invalid url") {
		t.Fatalf("want an invalid url error, got %+v", results)
	}
}

func TestResolveURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    string
		wantErr string
	}{
		{name: "relative joins base", rawURL: "api.yaml", want: filepath.Join("/base", "api.yaml")},
		{name: "absolute kept", rawURL: "/abs/api.yaml", want: "/abs/api.yaml"},
		{name: "file scheme", rawURL: "file:///tmp/api.yaml", want: "/tmp/api.yaml"},
		{name: "empty", rawURL: "", wantErr: "missing url"},
		{name: "unparseable", rawURL: ":", wantErr: "invalid url"},
		{name: "http has no local path", rawURL: "http://x/api.yaml", wantErr: "remote url has no local path"},
		{name: "unsupported scheme", rawURL: "ftp://x/api.yaml", wantErr: `unsupported url scheme "ftp"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ResolveURL(tt.rawURL, "/base")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("want error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
