package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/emmanuelperu/arazzo-maestro/internal/hurlgen"
	"github.com/emmanuelperu/arazzo-maestro/internal/k6gen"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/oasresolver"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Generate executable tests from Arazzo workflows",
		Long: `Generate executable test artifacts from an Arazzo workflow file.

Two kinds of generation are available:

  e2e   end-to-end functional tests        (formats: hurl)
  perf  load and performance tests         (formats: k6)

Each kind is a subcommand of 'test gen'; the output technology is
picked through '--format'. Kind-specific options (e.g. virtual users
and duration for perf) live under the relevant subcommand so the
help text stays scoped to what is actually useful for that kind.

'test gen' writes the test files to disk; 'test run' generates them on
the fly and executes them against a live endpoint, optionally emitting
an HTML report.`,
	}
	cmd.AddCommand(newTestGenCmd(), newTestRunCmd())
	return cmd
}

func newTestGenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate test files from an Arazzo file",
	}
	cmd.AddCommand(newTestGenE2ECmd(), newTestGenPerfCmd())
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

type testGenPerfOptions struct {
	output     string
	workflowID string
	format     string
	vus        int
	duration   string
	thresholds []string
}

func newTestGenPerfCmd() *cobra.Command {
	opts := &testGenPerfOptions{}
	cmd := &cobra.Command{
		Use:   "perf <file>",
		Short: "Generate load/performance test files from an Arazzo workflow",
		Long: `Generate load/performance test files from an Arazzo workflow file.

Each workflow becomes one test file under the output directory:

  <-o>/perf/<format>/<arazzo-name>/<workflowId>.k6.js

Supported formats:

  k6      a k6 script (.k6.js); runs via the single 'k6' binary

The load profile and thresholds are not part of Arazzo, so they are
supplied here and written into the script's exported 'options':

  --vus        concurrent virtual users
  --duration   how long to run (e.g. 30s, 5m)
  --threshold  a k6 threshold as 'metric=expression' (repeatable);
               a bare 'expression' defaults the metric to
               http_req_duration

The generated script reads its target from the BASE_URL environment
variable (default: the OpenAPI servers URL), and each workflow input
from a same-named variable, so the same script runs against any
environment:

  k6 run -e BASE_URL=https://staging.example.com -e productId=p-001 out.k6.js

Operation resolution uses the OpenAPI documents declared under
'sourceDescriptions' (local files only). Steps whose operationId cannot
be resolved emit a placeholder request and a comment naming the missing
id, so the script stays valid JavaScript a human can patch.

Examples:

  arazzo-maestro test gen perf shop.arazzo.yaml -o dist/ --vus=10 --duration=30s
  arazzo-maestro test gen perf shop.arazzo.yaml -o dist/ \
    --threshold='http_req_duration=p(95)<500' --threshold='http_req_failed=rate<0.01'`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTestGenPerf(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "dist", "Output directory")
	cmd.Flags().StringVar(&opts.workflowID, "workflow", "", "Only generate this workflow (default: all)")
	cmd.Flags().StringVar(&opts.format, "format", "k6", "Output format (currently only 'k6')")
	cmd.Flags().IntVar(&opts.vus, "vus", 1, "Concurrent virtual users")
	cmd.Flags().StringVar(&opts.duration, "duration", "30s", "Test duration (e.g. 30s, 5m)")
	cmd.Flags().StringArrayVar(&opts.thresholds, "threshold", nil, "k6 threshold as 'metric=expression' (repeatable)")
	return cmd
}

func runTestGenPerf(cmd *cobra.Command, path string, opts *testGenPerfOptions) error {
	files, err := generatePerfFiles(opts, path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no workflows found in %s\n", path)
		return nil
	}
	for _, f := range files {
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", f)
	}
	return nil
}

// generatePerfFiles parses the Arazzo file, resolves its OpenAPI sources,
// and writes one k6 script per selected workflow under
// <output>/perf/<format>/<arazzo-name>/.
func generatePerfFiles(opts *testGenPerfOptions, path string) ([]string, error) {
	if opts.format != "k6" {
		return nil, fmt.Errorf("unsupported format %q (supported: k6)", opts.format)
	}
	thresholds, err := parseThresholds(opts.thresholds)
	if err != nil {
		return nil, err
	}
	doc, err := parser.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	sources, err := loadArazzoSources(doc, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	workflows, err := selectWorkflows(doc, opts.workflowID)
	if err != nil {
		return nil, err
	}
	if len(workflows) == 0 {
		return nil, nil
	}
	outDir := filepath.Join(opts.output, "perf", opts.format, arazzoBaseName(path))
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", outDir, err)
	}
	genOpts := k6gen.Options{VUs: opts.vus, Duration: opts.duration, Thresholds: thresholds}
	var files []string
	for _, wf := range workflows {
		body, err := k6gen.Generate(wf, sources, genOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to generate workflow %q: %w", wf.WorkflowID, err)
		}
		outPath := filepath.Join(outDir, wf.WorkflowID+".k6.js")
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", outPath, err)
		}
		files = append(files, outPath)
	}
	return files, nil
}

// parseThresholds turns the repeatable --threshold flag into a k6
// thresholds map. Each entry is 'metric=expression'; a bare 'expression'
// (no '=') defaults the metric to http_req_duration. Multiple entries
// for the same metric accumulate.
func parseThresholds(raw []string) (map[string][]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string][]string)
	for _, t := range raw {
		metric, expr := "http_req_duration", t
		if i := strings.Index(t, "="); i >= 0 {
			metric, expr = strings.TrimSpace(t[:i]), strings.TrimSpace(t[i+1:])
		}
		if metric == "" || expr == "" {
			return nil, fmt.Errorf("invalid --threshold %q (expected 'metric=expression')", t)
		}
		out[metric] = append(out[metric], expr)
	}
	return out, nil
}

func newTestRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Generate tests and run them against a live endpoint",
	}
	cmd.AddCommand(newTestRunE2ECmd())
	return cmd
}

type testRunE2EOptions struct {
	baseURL    string
	reportHTML string
	workflowID string
	format     string
	variables  []string
}

func newTestRunE2ECmd() *cobra.Command {
	opts := &testRunE2EOptions{}
	cmd := &cobra.Command{
		Use:   "e2e <file>",
		Short: "Generate and execute end-to-end API tests against an endpoint",
		Long: `Generate end-to-end tests from an Arazzo workflow and run them
immediately against a live endpoint.

The tests are generated to a temporary directory and executed with the
'hurl' binary (which must be on PATH). The target environment is chosen
at run time with --base-url, so the same workflow runs unchanged against
staging, pre-production or a local mock:

  arazzo-maestro test run e2e shop.arazzo.yaml --base-url https://staging.example.com/api/v1

Pass --report-html to also write Hurl's HTML report:

  arazzo-maestro test run e2e shop.arazzo.yaml \
    --base-url https://staging.example.com/api/v1 \
    --report-html dist/hurl-report

Workflow inputs are supplied as Hurl variables with --variable (the flag
is repeatable); the generated file header lists the inputs each workflow
expects:

  arazzo-maestro test run e2e shop.arazzo.yaml \
    --base-url http://localhost:8080 \
    --variable productId=p-001 --variable orderId=ord-1

The process exit status mirrors hurl: non-zero when any test fails. When
--report-html is set the report is written even on failure.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTestRunE2E(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.baseURL, "base-url", "", "Target endpoint, e.g. https://staging.example.com/api/v1 (required)")
	cmd.Flags().StringVar(&opts.reportHTML, "report-html", "", "Also write a Hurl HTML report to this directory")
	cmd.Flags().StringVar(&opts.workflowID, "workflow", "", "Only run this workflow (default: all)")
	cmd.Flags().StringVar(&opts.format, "format", "hurl", "Test format (currently only 'hurl')")
	cmd.Flags().StringArrayVar(&opts.variables, "variable", nil, "Extra Hurl variable as name=value for workflow inputs (repeatable)")
	_ = cmd.MarkFlagRequired("base-url")
	return cmd
}

func runTestRunE2E(cmd *cobra.Command, path string, opts *testRunE2EOptions) error {
	if opts.format != "hurl" {
		return fmt.Errorf("unsupported format %q (supported: hurl)", opts.format)
	}
	if err := validateBaseURL(opts.baseURL); err != nil {
		return err
	}
	if _, err := exec.LookPath("hurl"); err != nil {
		return fmt.Errorf("the 'hurl' binary is required to run tests but was not found in PATH; install it from https://hurl.dev (e.g. `brew install hurl`)")
	}

	// The generated .hurl files are an execution detail here, not an
	// artifact the caller asked to keep, so they go to a temp dir that
	// is removed once hurl has run.
	tmp, err := os.MkdirTemp("", "arazzo-e2e-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()

	files, err := generateE2EFiles(e2eGenSpec{
		output:     tmp,
		workflowID: opts.workflowID,
		format:     opts.format,
	}, path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no workflows found in %s\n", path)
		return nil
	}

	hurlArgs := []string{"--test", "--variable", "baseUrl=" + opts.baseURL}
	for _, v := range opts.variables {
		hurlArgs = append(hurlArgs, "--variable", v)
	}
	if opts.reportHTML != "" {
		hurlArgs = append(hurlArgs, "--report-html", opts.reportHTML)
	}
	hurlArgs = append(hurlArgs, files...)

	hurl := exec.Command("hurl", hurlArgs...)
	hurl.Stdout = cmd.OutOrStdout()
	hurl.Stderr = cmd.ErrOrStderr()
	runErr := hurl.Run()
	if opts.reportHTML != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "\nHTML report: %s\n", filepath.Join(opts.reportHTML, "index.html"))
	}
	if runErr != nil {
		return fmt.Errorf("hurl reported test failures: %w", runErr)
	}
	return nil
}

// validateBaseURL rejects an endpoint that is not an absolute http(s)
// URL before we hand it to hurl, so the failure is a clear CLI error
// rather than a wall of hurl connection noise.
func validateBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid --base-url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("--base-url must be an http(s) URL, got %q", raw)
	}
	if u.Host == "" {
		return fmt.Errorf("--base-url must include a host, got %q", raw)
	}
	return nil
}

func runTestGenE2E(cmd *cobra.Command, path string, opts *testGenE2EOptions) error {
	files, err := generateE2EFiles(e2eGenSpec{
		output:     opts.output,
		workflowID: opts.workflowID,
		format:     opts.format,
	}, path)
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: no workflows found in %s\n", path)
		return nil
	}
	for _, f := range files {
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", f)
	}
	return nil
}

// e2eGenSpec is the shared input for generating e2e test files; both the
// 'gen' and 'run' subcommands resolve sources and render workflows the
// same way, differing only in where the output lands and what happens
// next.
type e2eGenSpec struct {
	output     string
	workflowID string
	format     string
}

// generateE2EFiles parses the Arazzo file, resolves its OpenAPI sources,
// and writes one test file per selected workflow under
// <output>/e2e/<format>/<arazzo-name>/. It returns the paths written, or
// an empty slice when the file declares no (matching) workflow.
func generateE2EFiles(spec e2eGenSpec, path string) ([]string, error) {
	if spec.format != "hurl" {
		return nil, fmt.Errorf("unsupported format %q (supported: hurl)", spec.format)
	}
	doc, err := parser.ParseFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	sources, err := loadArazzoSources(doc, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	workflows, err := selectWorkflows(doc, spec.workflowID)
	if err != nil {
		return nil, err
	}
	if len(workflows) == 0 {
		return nil, nil
	}
	outDir := filepath.Join(spec.output, "e2e", spec.format, arazzoBaseName(path))
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create %s: %w", outDir, err)
	}
	var files []string
	for _, wf := range workflows {
		body, err := hurlgen.Generate(wf, sources)
		if err != nil {
			return nil, fmt.Errorf("failed to generate workflow %q: %w", wf.WorkflowID, err)
		}
		outPath := filepath.Join(outDir, wf.WorkflowID+".hurl")
		if err := os.WriteFile(outPath, []byte(body), 0o644); err != nil {
			return nil, fmt.Errorf("failed to write %s: %w", outPath, err)
		}
		files = append(files, outPath)
	}
	return files, nil
}

// selectWorkflows returns all workflows, or just the one matching
// workflowID. An unknown id is an error listing the available ids.
func selectWorkflows(doc *model.ArazzoDocument, workflowID string) ([]model.Workflow, error) {
	if workflowID == "" {
		return doc.Workflows, nil
	}
	var filtered []model.Workflow
	for _, w := range doc.Workflows {
		if w.WorkflowID == workflowID {
			filtered = append(filtered, w)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("workflow %q not found. Available: %s", workflowID, availableWorkflows(doc))
	}
	return filtered, nil
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
