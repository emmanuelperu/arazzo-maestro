package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/emmanuelperu/arazzo-maestro/internal/hurlgen"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Generate executable tests from Arazzo workflows",
		Long: `Generate executable test artifacts from an Arazzo workflow file.

Two kinds of generation are planned:

  e2e   end-to-end functional tests        (formats: hurl)
  perf  load and performance tests         (formats: k6, drill)

Each kind is a subcommand of 'test gen'; the output technology is
picked through '--format'. Kind-specific options (e.g. virtual users
and duration for perf) live under the relevant subcommand so the
help text stays scoped to what is actually useful for that kind.`,
	}
	cmd.AddCommand(newTestGenCmd())
	return cmd
}

func newTestGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate test files from an Arazzo file",
	}
	cmd.AddCommand(newTestGenE2ECmd())
	return cmd
}

type testGenE2EOptions struct {
	output     string
	workflowID string
	format     string
}

func newTestGenE2ECmd() *cobra.Command {
	opts := &testGenE2EOptions{}
	cmd := &cobra.Command{
		Use:   "e2e <file>",
		Short: "Generate end-to-end API test files from an Arazzo workflow",
		Long: `Generate end-to-end API test files from an Arazzo workflow file.

Each workflow becomes one test file under the output directory:

  <-o>/e2e/<format>/<arazzo-name>/<workflowId>.<ext>

The 'e2e/<format>/<arazzo-name>/' prefix is added automatically so a
single output directory can hold artifacts for several Arazzo files
and several kinds (e2e, perf, ...) without collisions, and so the
on-disk layout mirrors the subcommand grammar.

Supported formats:

  hurl    declarative .hurl files; runs via the single 'hurl' binary

Operation resolution uses the OpenAPI documents declared under
'sourceDescriptions' in the Arazzo file (loaded as local files only;
HTTP/HTTPS URLs are rejected). Steps whose operationId cannot be
resolved emit a placeholder request line and a comment naming the
missing id, so the output stays valid Hurl that a human can patch.

Arazzo runtime expressions are translated to Hurl templates where
possible: $inputs.foo becomes {{foo}}, $steps.s.outputs.o becomes
{{s_o}}, $response.body#/x becomes a jsonpath capture. Unknown forms
pass through unchanged with a comment.

Examples:

  arazzo-maestro test gen e2e shop.arazzo.yaml -o dist/
  arazzo-maestro test gen e2e shop.arazzo.yaml -o dist/ --workflow=happy-path
  arazzo-maestro test gen e2e shop.arazzo.yaml -o dist/ --format=hurl`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTestGenE2E(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "dist", "Output directory")
	cmd.Flags().StringVar(&opts.workflowID, "workflow", "", "Only generate this workflow (default: all)")
	cmd.Flags().StringVar(&opts.format, "format", "hurl", "Output format (currently only 'hurl')")
	return cmd
}

func runTestGenE2E(cmd *cobra.Command, path string, opts *testGenE2EOptions) error {
	if opts.format != "hurl" {
		return fmt.Errorf("unsupported format %q (supported: hurl)", opts.format)
	}
	doc, err := parser.ParseFile(path)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	sources, err := loadArazzoSources(doc, filepath.Dir(path))
	if err != nil {
		return err
	}

	workflows := doc.Workflows
	if opts.workflowID != "" {
		filtered := workflows[:0:0]
		for _, w := range workflows {
			if w.WorkflowID == opts.workflowID {
				filtered = append(filtered, w)
			}
		}
		if len(filtered) == 0 {
			return fmt.Errorf("workflow %q not found. Available: %s", opts.workflowID, availableWorkflows(doc))
		}
		workflows = filtered
	}
	if len(workflows) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no workflows found in %s\n", path)
		return nil
	}
	outDir := filepath.Join(opts.output, "e2e", opts.format, arazzoBaseName(path))
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("failed to create %s: %w", outDir, err)
	}
	for _, wf := range workflows {
		body, err := hurlgen.Generate(wf, sources)
		if err != nil {
			return fmt.Errorf("failed to generate workflow %q: %w", wf.WorkflowID, err)
		}
		outPath := filepath.Join(outDir, wf.WorkflowID+".hurl")
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			return fmt.Errorf("failed to write %s: %w", outPath, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", outPath)
	}
	return nil
}

// arazzoBaseName strips the conventional ".arazzo.yaml" / ".arazzo.yml"
// suffix from a file path, falling back to the regular extension strip
// when neither matches. Used to derive a per-source subdirectory under
// the generation output.
func arazzoBaseName(path string) string {
	name := filepath.Base(path)
	for _, ext := range []string{".arazzo.yaml", ".arazzo.yml"} {
		if strings.HasSuffix(name, ext) {
			return strings.TrimSuffix(name, ext)
		}
	}
	return strings.TrimSuffix(name, filepath.Ext(name))
}

func loadArazzoSources(doc *model.ArazzoDocument, basePath string) (map[string]*oasresolver.Source, error) {
	sources := make(map[string]*oasresolver.Source, len(doc.SourceDescriptions))
	for _, src := range doc.SourceDescriptions {
		// Only OpenAPI sources are resolvable today. Other types
		// (e.g. 'arazzo' for nested workflows) are skipped.
		if src.Type != "" && src.Type != "openapi" {
			continue
		}
		path, err := resolveSourceURL(src.URL, basePath)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", src.Name, err)
		}
		loaded, err := oasresolver.Load(path)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil, fmt.Errorf("source %q: file not found (resolved to %s)", src.Name, path)
			}
			return nil, fmt.Errorf("source %q: %w", src.Name, err)
		}
		sources[src.Name] = loaded
	}
	return sources, nil
}

// resolveSourceURL turns an Arazzo 'url:' field into an absolute local
// path. HTTP/HTTPS schemes are rejected: the CLI stays offline and
// deterministic, mirroring the linter's cross-file pass.
func resolveSourceURL(rawURL, basePath string) (string, error) {
	if rawURL == "" {
		return "", errors.New("missing url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return "", fmt.Errorf("HTTP source URLs are not supported (use a local file or relative path)")
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
