// Package lint implements gwlint's own "lint" command. kube-linter's own
// pkg/command/lint.Command() cannot be reused as-is: it calls
// lintcontext.CreateContexts directly, with no way to supply the custom
// decoder gwlint needs to parse Gateway API and Envoy Gateway objects. So
// this command reimplements that command's flow, reusing kube-linter's
// engine and output-formatting packages everywhere they don't hardcode
// kube-linter's own decoder.
package lint

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.stackrox.io/kube-linter/pkg/checkregistry"
	"golang.stackrox.io/kube-linter/pkg/command/common"
	kllint "golang.stackrox.io/kube-linter/pkg/command/lint"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/pathutil"
	"golang.stackrox.io/kube-linter/pkg/run"

	"github.com/sdOps/gwlint/pkg/builtinchecks"
	"github.com/sdOps/gwlint/pkg/decoder"
	gwversion "github.com/sdOps/gwlint/pkg/version"
)

const plainTemplateStr = `gwlint {{.Summary.KubeLinterVersion}}

{{range .Reports}}
{{- .Object.Metadata.FilePath | bold}}: (object: {{.Object.GetK8sObjectName | bold}}) {{.Diagnostic.Message | red}} (check: {{.Check | yellow}}, remediation: {{.Remediation | yellow}})

{{else}}No lint errors found!
{{end -}}
`

var (
	plainTemplate = common.MustInstantiatePlainTemplate(plainTemplateStr, nil)

	formatters = common.Formatters{
		Formatters: map[common.FormatType]common.FormatFunc{
			common.JSONFormat:  common.FormatJSON,
			common.PlainFormat: plainTemplate.Execute,
		},
	}
)

// Command is the command for the lint command.
func Command() *cobra.Command {
	var failIfNoObjects bool
	var verbose bool
	var formats []string
	var outputs []string

	c := &cobra.Command{
		Use:   "lint",
		Args:  cobra.MinimumNArgs(1),
		Short: "Lint Gateway API and Envoy Gateway YAML manifests",
		RunE: func(_ *cobra.Command, args []string) error {
			checkRegistry := checkregistry.New()
			if err := builtinchecks.LoadInto(checkRegistry); err != nil {
				return err
			}
			defaultChecks, err := builtinchecks.List()
			if err != nil {
				return err
			}
			enabledChecks := make([]string, 0, len(defaultChecks))
			for _, chk := range defaultChecks {
				enabledChecks = append(enabledChecks, chk.Name)
			}
			if len(enabledChecks) == 0 {
				fmt.Fprintln(os.Stderr, "Warning: no checks enabled.")
				return nil
			}

			absArgs := make([]string, 0, len(args))
			for _, arg := range args {
				if arg == lintcontext.ReadFromStdin {
					absArgs = append(absArgs, lintcontext.ReadFromStdin)
					continue
				}
				absArg, err := pathutil.GetAbsolutPath(arg)
				if err != nil {
					return err
				}
				absArgs = append(absArgs, absArg)
			}

			lintCtxs, err := lintcontext.CreateContextsWithOptions(
				lintcontext.Options{CustomDecoder: decoder.Decoder}, nil, absArgs...)
			if err != nil {
				return err
			}
			// Merge every discovered context into one: see merge.go for why.
			lintCtxs = []lintcontext.LintContext{mergeContexts(lintCtxs)}

			if verbose {
				for _, lintCtx := range lintCtxs {
					for _, invalidObj := range lintCtx.InvalidObjects() {
						_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to load object from %s: %v\n",
							invalidObj.Metadata.FilePath, invalidObj.LoadErr)
					}
				}
			}

			var atLeastOneObjectFound bool
			for _, lintCtx := range lintCtxs {
				if len(lintCtx.Objects()) > 0 {
					atLeastOneObjectFound = true
					break
				}
			}
			if !atLeastOneObjectFound {
				msg := "no valid objects found"
				if failIfNoObjects {
					return errors.New(msg)
				}
				fmt.Fprintf(os.Stderr, "Warning: %s.\n", msg)
				return nil
			}

			result, err := run.Run(lintCtxs, checkRegistry, enabledChecks)
			if err != nil {
				return err
			}
			result.Summary.KubeLinterVersion = gwversion.Get()

			pairs, err := kllint.ValidateAndPairFormatsOutputs(formats, outputs, formatters.GetEnabledFormatters())
			if err != nil {
				return err
			}

			var writeErrors []error
			for _, pair := range pairs {
				formatter, err := formatters.FormatterByType(string(pair.Format))
				if err != nil {
					return err
				}
				dest, err := kllint.NewOutputDestination(pair.Output)
				if err != nil {
					writeErrors = append(writeErrors, fmt.Errorf("failed to create output destination for %s: %w", pair.Format, err))
					continue
				}
				writeErr := formatter(dest.Writer, result)
				closeErr := dest.Close()
				if writeErr != nil {
					writeErrors = append(writeErrors, fmt.Errorf("formatting failed for %s: %w", pair.Format, writeErr))
					continue
				}
				if closeErr != nil {
					writeErrors = append(writeErrors, fmt.Errorf("failed to close output for %s: %w", pair.Format, closeErr))
				}
			}
			if len(writeErrors) > 0 {
				var errMsg strings.Builder
				fmt.Fprintf(&errMsg, "failed to write %d of %d output format(s):\n", len(writeErrors), len(pairs))
				for i, err := range writeErrors {
					fmt.Fprintf(&errMsg, "  %d. %v\n", i+1, err)
				}
				return errors.New(errMsg.String())
			}

			if len(result.Reports) > 0 {
				return fmt.Errorf("found %d lint errors", len(result.Reports))
			}
			return nil
		},
	}

	c.Flags().BoolVarP(&failIfNoObjects, "fail-if-no-objects-found", "", false,
		"Return non-zero exit code if no valid objects are found or failed to parse")
	c.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging")
	c.Flags().StringSliceVar(&formats, "format", []string{common.PlainFormat},
		fmt.Sprintf("Output format (can be repeated). Allowed values: %s",
			strings.Join(formatters.GetEnabledFormatters(), ", ")))
	c.Flags().StringSliceVar(&outputs, "output", []string{},
		"Output file path (can be repeated). Must match the number of --format flags. "+
			"If omitted, all outputs go to stdout")
	return c
}
