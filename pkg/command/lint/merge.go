package lint

import (
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
)

// mergedLintContext combines every object and invalid object from a set of
// LintContexts into one. kube-linter scopes each LintContext to the
// directory its files were loaded from (see lintcontext.CreateContexts),
// which is the wrong default for Gateway API: a Gateway, its routes, its
// BackendTrafficPolicies, and its Backends are routinely authored across
// separate files, separate Helm charts, and separate repos. A check like
// fqdn-backend-cold-start needs to see all of them at once to mean
// anything, so gwlint's lint command merges every discovered context
// before running checks, rather than keeping kube-linter's per-directory
// isolation.
type mergedLintContext struct {
	objects        []lintcontext.Object
	invalidObjects []lintcontext.InvalidObject
}

func (m *mergedLintContext) Objects() []lintcontext.Object {
	return m.objects
}

func (m *mergedLintContext) InvalidObjects() []lintcontext.InvalidObject {
	return m.invalidObjects
}

// mergeContexts combines contexts into a single LintContext containing
// every object and invalid object across all of them, regardless of which
// directory each one was originally loaded from.
func mergeContexts(contexts []lintcontext.LintContext) lintcontext.LintContext {
	merged := &mergedLintContext{}
	for _, ctx := range contexts {
		merged.objects = append(merged.objects, ctx.Objects()...)
		merged.invalidObjects = append(merged.invalidObjects, ctx.InvalidObjects()...)
	}
	return merged
}
