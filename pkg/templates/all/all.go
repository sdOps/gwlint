// Package all imports every check template so that importing this one package
// registers the lot. Mirrors kube-linter's own pkg/templates/all.
//
// A check is only registered by the init() of its own package, so anything
// that runs checks has to import each one. Keeping that list here means adding
// a check touches one file rather than the binary and every test that needs a
// populated registry.
package all

import (
	// Each check package registers its template via init().
	_ "github.com/sdOps/gwlint/pkg/templates/danglingbackendref"
	_ "github.com/sdOps/gwlint/pkg/templates/danglingparentref"
	_ "github.com/sdOps/gwlint/pkg/templates/fqdnbackendcoldstart"
)
