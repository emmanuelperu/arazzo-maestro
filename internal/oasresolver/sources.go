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
	Path   string // resolved local path, "" when resolution failed
	Source *Source
	Err    error
}

// LoadAll resolves and loads every OpenAPI sourceDescription against
// basePath, returning one result per attempted source so callers can
// either accumulate errors or fail fast. Non-openapi source types are
// skipped.
func LoadAll(descs []model.SourceDescription, basePath string) []SourceResult {
	results := make([]SourceResult, 0, len(descs))
	for _, src := range descs {
		if src.Type != "" && src.Type != "openapi" {
			continue
		}
		r := SourceResult{Name: src.Name, URL: src.URL}
		r.Path, r.Err = ResolveURL(src.URL, basePath)
		if r.Err == nil {
			r.Source, r.Err = Load(r.Path)
		}
		results = append(results, r)
	}
	return results
}

// ResolveURL turns an Arazzo source url into an absolute local path.
// HTTP/HTTPS schemes are rejected: loading stays offline and
// deterministic.
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
		return "", fmt.Errorf(
			"HTTP source URLs are not supported\n\turl: %s\n\thint: use a local file or a relative path",
			rawURL,
		)
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
