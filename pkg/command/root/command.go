// Package root assembles gwlint's CLI. kube-linter's own root command
// (golang.stackrox.io/kube-linter/pkg/command/root) cannot be reused
// wholesale: its "lint" subcommand hardcodes kube-linter's decoder, and its
// "checks"/"version" subcommands report on kube-linter's own built-in
// checks and version, not gwlint's. Its "templates" subcommand, however,
// operates on the shared golang.stackrox.io/kube-linter/pkg/templates
// registry, and correctly lists only what gwlint itself has registered
// into that registry, so it's reused here as-is.
package root

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	kltemplates "golang.stackrox.io/kube-linter/pkg/command/templates"

	gwlintcmd "github.com/sdOps/gwlint/pkg/command/lint"
	gwversion "github.com/sdOps/gwlint/pkg/version"
)

// Command is gwlint's root command.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use:           filepath.Base(os.Args[0]),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	c.AddCommand(
		gwlintcmd.Command(),
		kltemplates.Command(),
		versionCommand(),
	)
	return c
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version and exit",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(gwversion.Get())
		},
	}
}
