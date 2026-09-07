// Command gwlint is a static analysis tool for Kubernetes Gateway API and
// Envoy Gateway resources. It is built on kube-linter's engine packages;
// see the README for architecture and attribution.
package main

import (
	"os"

	"github.com/sdOps/gwlint/pkg/command/root"

	// Blank-imported so the check template registers itself via init().
	_ "github.com/sdOps/gwlint/pkg/templates/fqdnbackendcoldstart"
)

func main() {
	c := root.Command()
	if err := c.Execute(); err != nil {
		os.Exit(1)
	}
}
