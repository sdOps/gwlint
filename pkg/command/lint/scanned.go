package lint

import (
	"fmt"
	"sort"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/instantiatedcheck"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/run"
)

// lintResult is the run result plus what the run actually looked at. Without
// the latter, "No lint errors found" reads the same whether every manifest was
// checked and came back clean or nothing a check applies to was ever loaded,
// and the reader has no way to tell which one they are looking at.
type lintResult struct {
	run.Result
	Scanned scanSummary
}

// scanSummary counts what reached the checks.
//
// Checked is the figure that matters, not Objects: kube-linter decodes a kind
// it does not recognise into an unstructured object rather than rejecting it,
// so objects load whether or not gwlint understands them, and a large Objects
// count on its own does not mean a check ever ran.
type scanSummary struct {
	// Objects and Files are everything that loaded.
	Objects int
	Files   int
	// Checked is how many objects a check was actually run against, and
	// CheckedByKind breaks that down by kind for the report.
	Checked       int
	CheckedByKind map[string]int
	// LooksFor is every object kind the enabled checks are scoped to, used to
	// say what was missing when nothing matched.
	LooksFor []string
	// Unparsed is what failed to decode entirely.
	Unparsed int
}

func summarize(lintCtxs []lintcontext.LintContext, checks []*instantiatedcheck.InstantiatedCheck) scanSummary {
	s := scanSummary{CheckedByKind: map[string]int{}}

	kinds := map[string]struct{}{}
	for _, check := range checks {
		for _, kind := range check.Spec.Scope.ObjectKinds {
			kinds[kind] = struct{}{}
		}
	}
	for kind := range kinds {
		s.LooksFor = append(s.LooksFor, kind)
	}
	sort.Strings(s.LooksFor)

	files := make(map[string]struct{})
	for _, ctx := range lintCtxs {
		for _, obj := range ctx.Objects() {
			s.Objects++
			if path := obj.Metadata.FilePath; path != "" {
				files[path] = struct{}{}
			}
			gvk := obj.K8sObject.GetObjectKind().GroupVersionKind()
			for _, check := range checks {
				if check.Matcher.Matches(gvk) {
					s.Checked++
					s.CheckedByKind[gvk.Kind]++
					break
				}
			}
		}
		for _, obj := range ctx.InvalidObjects() {
			s.Unparsed++
			if path := obj.Metadata.FilePath; path != "" {
				files[path] = struct{}{}
			}
		}
	}
	s.Files = len(files)
	return s
}

// String renders the line the plain output ends with. It names the kinds
// rather than talking about scope, so a reader who has never seen gwlint
// before can tell what it looked at and, when it looked at nothing, what it
// was looking for.
func (s scanSummary) String() string {
	loaded := fmt.Sprintf("%s loaded from %s",
		plural(s.Objects, "object", "objects"), plural(s.Files, "file", "files"))

	var line string
	switch {
	case s.Checked == 0:
		line = fmt.Sprintf("No %s found, so nothing was checked (%s).", joinKinds(s.LooksFor), loaded)
	case len(s.CheckedByKind) == 1:
		line = fmt.Sprintf("Checked %s (%s).", soleKind(s.CheckedByKind), loaded)
	default:
		line = fmt.Sprintf("Checked %s: %s (%s).",
			plural(s.Checked, "object", "objects"), byKind(s.CheckedByKind), loaded)
	}

	if s.Unparsed > 0 {
		line += fmt.Sprintf(" %s could not be parsed and %s not checked; re-run with --verbose to see them.",
			plural(s.Unparsed, "object", "objects"), were(s.Unparsed))
	}
	return line
}

// soleKind renders the single-kind case as "2 BackendTrafficPolicy objects".
func soleKind(byKind map[string]int) string {
	for kind, n := range byKind {
		return fmt.Sprintf("%d %s %s", n, kind, objectNoun(n))
	}
	return ""
}

// byKind renders "49 HTTPRoute, 2 BackendTrafficPolicy", commonest first.
func byKind(counts map[string]int) string {
	kinds := make([]string, 0, len(counts))
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(i, j int) bool {
		if counts[kinds[i]] != counts[kinds[j]] {
			return counts[kinds[i]] > counts[kinds[j]]
		}
		return kinds[i] < kinds[j]
	})
	parts := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
	}
	return strings.Join(parts, ", ")
}

// joinKinds renders the kinds gwlint looks for as "BackendTrafficPolicy
// objects" or "BackendTrafficPolicy or HTTPRoute objects".
func joinKinds(kinds []string) string {
	switch len(kinds) {
	case 0:
		return "checkable objects"
	case 1:
		return kinds[0] + " objects"
	case 2:
		return kinds[0] + " or " + kinds[1] + " objects"
	default:
		return strings.Join(kinds[:len(kinds)-1], ", ") + " or " + kinds[len(kinds)-1] + " objects"
	}
}

func objectNoun(n int) string {
	if n == 1 {
		return "object"
	}
	return "objects"
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func were(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}
