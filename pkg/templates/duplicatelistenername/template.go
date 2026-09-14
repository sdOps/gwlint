// Package duplicatelistenername flags a Gateway or ListenerSet declaring two
// listeners under the same name. The name is the only handle anything else
// has on a listener: a route's parentRef sectionName and a listener-scoped
// policy targetRef both address one by name, so a name that covers two
// listeners addresses neither.
//
// Uniqueness is expressed upstream as `+listType=map` plus a CEL rule
// ("Listener name must be unique within the Gateway"). Both are Kubernetes
// extensions rather than JSON Schema constraints, so kubeconform and the
// schema validation built into helm and kustomize accept the manifest and the
// rejection lands at apply time, in a deploy job rather than in review.
//
// The check reads one object's own listeners and resolves nothing across
// objects, so it needs no partial-manifest-set guard: linting a Gateway on its
// own gives it everything it needs, and no absent object can make it fire.
package duplicatelistenername

import (
	"fmt"
	"sort"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "duplicate-listener-name"

func init() {
	templates.Register(check.Template{
		HumanName: "Duplicate Listener Name",
		Key:       templateKey,
		Description: "Flag Gateways and ListenerSets that declare two listeners under the same name, " +
			"which a route sectionName or a listener-scoped policy cannot then address unambiguously",
		SupportedObjectKinds: config.ObjectKindsDesc{
			// A ListenerSet carries the same uniqueness rule as a Gateway, and
			// its listeners are addressed by name the same way.
			ObjectKinds: []string{gwobjectkinds.Gateway, gwobjectkinds.ListenerSet},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	listeners := gatewayapi.ListenersOf(object.K8sObject)
	if len(listeners) < 2 {
		return nil
	}

	counts := make(map[gatewayv1.SectionName]int, len(listeners))
	for _, l := range listeners {
		counts[l.Name]++
	}

	duplicated := make([]string, 0, len(counts))
	for name, n := range counts {
		if n > 1 {
			duplicated = append(duplicated, string(name))
		}
	}
	// Map iteration order is not stable, and a diagnostic set that reorders
	// between runs is unreadable in a diff.
	sort.Strings(duplicated)

	owner := listeners[0]
	var diagnostics []diagnostic.Diagnostic
	for _, name := range duplicated {
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q declares %d listeners named %q; a listener name must be unique within a %s, "+
					"so the API server rejects the object and a route sectionName naming that listener "+
					"has no single listener to attach to",
				owner.OwnerKind, owner.Owner.Name, counts[gatewayv1.SectionName(name)], name, owner.OwnerKind),
		})
	}
	return diagnostics
}
