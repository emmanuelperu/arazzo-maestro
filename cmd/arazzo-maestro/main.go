// Command arazzo-maestro is a CLI for inspecting and rendering Arazzo
// workflow specifications.
//
//	arazzo-maestro lint shop.arazzo.yaml
//	arazzo-maestro view shop.arazzo.yaml --output dist/
//	arazzo-maestro view shop.arazzo.yaml --theme dark
//	arazzo-maestro view --list-themes
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/emmanuelperu/arazzo-maestro/internal/linter"
	"github.com/emmanuelperu/arazzo-maestro/internal/model"
	"github.com/emmanuelperu/arazzo-maestro/internal/parser"
	"github.com/emmanuelperu/arazzo-maestro/internal/renderer"
	"github.com/emmanuelperu/arazzo-maestro/internal/theme"
)

// defaultThemesFile is the auto-discovered themes config at the project
// root. Bypass it with --themes <path>.
const defaultThemesFile = "themes.yml"

// version is overridden at build time via -ldflags.
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "arazzo-maestro",
		Short:         "Inspect and render Arazzo workflow specifications",
		SilenceUsage:  true,
		SilenceErrors: false,
		Version:       version,
	}
	root.AddCommand(newLintCmd(), newViewCmd())
	return root
}

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint <file>",
		Short: "Validate an Arazzo YAML file against built-in rules",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLint(cmd, args[0])
		},
	}
}

func runLint(cmd *cobra.Command, path string) error {
	issues, err := linter.LintFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	if len(issues) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "OK: %s, no issues found\n", path)
		return nil
	}
	hasError := false
	for _, issue := range issues {
		fmt.Fprintln(cmd.OutOrStdout(), issue.String())
		if issue.Severity == linter.SeverityError {
			hasError = true
		}
	}
	if hasError {
		return fmt.Errorf("%d issue(s) found", len(issues))
	}
	return nil
}

type viewOptions struct {
	output     string
	workflowID string
	noIndex    bool
	themeName  string
	themesFile string
	listThemes bool
}

func newViewCmd() *cobra.Command {
	opts := &viewOptions{}

	cmd := &cobra.Command{
		Use:   "view <file>",
		Short: "Generate standalone HTML pages from an Arazzo YAML file",
		Args: func(cmd *cobra.Command, args []string) error {
			if opts.listThemes {
				return nil
			}
			return cobra.ExactArgs(1)(cmd, args)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if opts.listThemes {
				return runListThemes(cmd, opts)
			}
			return runView(cmd, args[0], opts)
		},
	}
	cmd.Flags().StringVarP(&opts.output, "output", "o", "dist", "Output directory")
	cmd.Flags().StringVar(&opts.workflowID, "workflow", "", "Only generate this workflow (default: all)")
	cmd.Flags().BoolVar(&opts.noIndex, "no-index", false, "Skip generating index.html")
	cmd.Flags().StringVar(&opts.themeName, "theme", "", "Theme name (default: built-in 'light', or 'default:' from ./themes.yml)")
	cmd.Flags().StringVar(&opts.themesFile, "themes", "", "Path to a themes YAML file (bypasses ./themes.yml auto-discovery)")
	cmd.Flags().BoolVar(&opts.listThemes, "list-themes", false, "List available themes and exit")
	return cmd
}

func loadThemes(cmd *cobra.Command, opts *viewOptions) (*theme.Registry, error) {
	r, err := theme.LoadBuiltin()
	if err != nil {
		return nil, err
	}
	switch {
	case opts.themesFile != "":
		if err := r.MergeFile(opts.themesFile); err != nil {
			return nil, err
		}
	default:
		if _, statErr := os.Stat(defaultThemesFile); statErr == nil {
			if err := r.MergeFile(defaultThemesFile); err != nil {
				return nil, err
			}
		} else if !errors.Is(statErr, fs.ErrNotExist) {
			return nil, statErr
		}
	}
	for _, w := range r.Audit() {
		fmt.Fprintln(cmd.ErrOrStderr(), "warning:", w.String())
	}
	return r, nil
}

func runListThemes(cmd *cobra.Command, opts *viewOptions) error {
	r, err := loadThemes(cmd, opts)
	if err != nil {
		return err
	}
	for _, name := range r.List() {
		marker := ""
		if name == r.Default {
			marker = " (default)"
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s%s\n", name, marker)
	}
	return nil
}

func runView(cmd *cobra.Command, path string, opts *viewOptions) error {
	doc, err := parser.ParseFile(path)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}

	registry, err := loadThemes(cmd, opts)
	if err != nil {
		return err
	}
	selected, err := registry.Resolve(opts.themeName)
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

	for _, wf := range workflows {
		out, err := renderer.WriteWorkflow(wf, opts.output, selected)
		if err != nil {
			return fmt.Errorf("failed to write workflow %q: %w", wf.WorkflowID, err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", out)
	}

	if !opts.noIndex && opts.workflowID == "" {
		out, err := renderer.WriteIndex(doc, opts.output, selected)
		if err != nil {
			return fmt.Errorf("failed to write index: %w", err)
		}
		fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", out)
	}
	return nil
}

func availableWorkflows(doc *model.ArazzoDocument) string {
	if doc == nil || len(doc.Workflows) == 0 {
		return "<none>"
	}
	names := make([]string, 0, len(doc.Workflows))
	for _, w := range doc.Workflows {
		names = append(names, w.WorkflowID)
	}
	return strings.Join(names, ", ")
}
