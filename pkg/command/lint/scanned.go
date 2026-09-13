package lint

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/instantiatedcheck"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/run"
)

// lintResult is the run result plus what the run actually looked at. Without
// the latter, "No lint errors found" reads the same whether every manifest was
// checked and came back clean or nothing relevant was ever loaded, and the
// reader has no way to tell which one they are looking at.
type lintResult struct {
	run.Result
	Scanned scanSummary
}

// scanSummary counts what reached the checks. InScope is the number that
// matched an enabled check's object kinds, which is the figure that says
// whether the run had anything to do: kube-linter decodes a kind it does not
// recognise into an unstructured object rather than rejecting it, so a high
// Objects count on its own does not mean a check ever ran.
type scanSummary struct {
	Objects  int
	InScope  int
	Files    int
	Unparsed int
}

func summarize(lintCtxs []lintcontext.LintContext, checks []*instantiatedcheck.InstantiatedCheck) scanSummary {
	var s scanSummary
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
					s.InScope++
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

// String renders the summary line the plain output ends with.
func (s scanSummary) String() string {
	line := fmt.Sprintf("Checked %s from %s",
		plural(s.Objects, "object", "objects"), plural(s.Files, "file", "files"))
	if s.InScope == 0 {
		line += ", none of which any enabled check applies to"
	} else {
		line += fmt.Sprintf(", %d in scope for the enabled checks", s.InScope)
	}
	line += "."
	if s.Unparsed > 0 {
		line += fmt.Sprintf(" %s could not be parsed and %s not checked; re-run with --verbose to see them.",
			plural(s.Unparsed, "object", "objects"), were(s.Unparsed))
	}
	return line
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
