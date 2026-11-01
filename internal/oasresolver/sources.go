package oasresolver

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/emmanuelperu/arazzo-maestro/internal/model"
)

// SourceResult is the outcome of loading one sourceDescription: either
// a loaded Source or the error that prevented it.
type SourceResult struct {
	Name   string
	URL    string
	Path   string // resolved local path or fetched HTTPS URL, "" when resolution failed
	Source *Source
	Err    error
}

// LoadAll resolves and loads every OpenAPI sourceDescription against
// basePath, returning one result per attempted source so callers can
// either accumulate errors or fail fast. Non-openapi source types are
// skipped. HTTPS sources are fetched only when remote.Allow is set.
func LoadAll(descs []model.SourceDescription, basePath string, remote RemoteOptions) []SourceResult {
	results := make([]SourceResult, 0, len(descs))
	for _, src := range descs {
		if src.Type != "" && src.Type != "openapi" {
			continue
		}
		r := SourceResult{Name: src.Name, URL: src.URL}
		if u, ok := remoteURL(src.URL); ok {
			r.Source, r.Err = loadRemote(u, remote)
			if r.Err == nil {
				r.Path = u.Redacted()
			}
		} else {
			r.Path, r.Err = ResolveURL(src.URL, basePath)
			if r.Err == nil {
				r.Source, r.Err = Load(r.Path)
			}
		}
		results = append(results, r)
	}
	return results
}

func loadRemote(u *url.URL, remote RemoteOptions) (*Source, error) {
	raw, err := fetch(u, remote)
	if err != nil {
		return nil, err
	}
	return parse(raw, u.Redacted())
}

// ResolveURL turns a local Arazzo source url into an absolute path.
// HTTP/HTTPS URLs have no local path and are rejected.
func ResolveURL(rawURL, basePath string) (string, error) {
	if rawURL == "" {
		return "", errors.New("missing url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return "", fmt.Errorf("remote url has no local path: %s", rawURL)
	case "file":
		return u.Path, nil
	case "":
		if filepath.IsAbs(rawURL) {
			return rawURL, nil
		}
		return filepath.Join(basePath, rawURL), nil
	}
	return "", fmt.Errorf("unsupported url scheme %q", u.Scheme)
}
