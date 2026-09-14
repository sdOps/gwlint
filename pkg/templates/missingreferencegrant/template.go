// Package missingreferencegrant flags a route whose backendRef crosses a
// namespace boundary with no ReferenceGrant in the target namespace allowing
// it. Gateway API treats a cross-namespace reference as denied by default: the
// owner of the target namespace has to opt in with a ReferenceGrant, otherwise
// the implementation resolves the backendRef to nothing and the rule serves
// errors. Both objects are individually valid and the apply succeeds, so a
// schema validator sees nothing wrong. The failure shows up as 500s after
// rollout, usually blamed on the backend rather than on the missing grant.
//
// Two things keep this quiet where it cannot know the answer.
//
// The first is an empty route namespace. Rendered manifests very often omit
// metadata.namespace because helm supplies it at install time, and an omitted
// backendRef namespace defaults to the route's own. With the route's namespace
// unknown there is no way to tell a cross-namespace reference from a local one,
// so nothing is reported.
//
// The second is the usual partial-manifest-set guard: a reference is judged
// only when the set contains at least one ReferenceGrant in the target
// namespace. Grants live with the workloads they protect, which is routinely a
// different chart or repo from the routes, and a target namespace the set says
// nothing about is not evidence that no grant exists.
package missingreferencegrant

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "missing-reference-grant"

// gatewayapiGroup is the group every route belongs to, which is what a grant's
// from.group has to say to cover one.
const gatewayapiGroup = gatewayapi.GroupName

func init() {
	templates.Register(check.Template{
		HumanName: "Missing ReferenceGrant",
		Key:       templateKey,
		Description: "Flag routes whose backendRefs cross a namespace boundary with no ReferenceGrant " +
			"in the target namespace permitting the reference, so the backend never resolves",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: gwobjectkinds.RouteKinds,
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return nil
	}
	// Without the route's own namespace, "same namespace" is undecidable and
	// every backendRef would look either local or remote by accident.
	if route.Namespace == "" {
		return nil
	}

	grants := grantsByNamespace(lintCtx)

	var diagnostics []diagnostic.Diagnostic
	for _, backendRef := range route.BackendRefs {
		target := backendRef.Namespace(route.Namespace)
		if target == route.Namespace {
			continue
		}
		// The grants for a namespace ship with that namespace's workloads. If
		// the set carries none, they were not passed in rather than absent.
		if len(grants[target]) == 0 {
			continue
		}
		ref := reference{
			fromKind:      string(route.Kind),
			fromNamespace: route.Namespace,
			toGroup:       backendRef.Group(),
			toKind:        backendRef.Kind(),
			toName:        string(backendRef.Ref.Name),
		}
		if permits(grants[target], ref) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q in namespace %q routes to %s %q in namespace %q, but no ReferenceGrant there "+
					"permits %s from namespace %q to reference it; the backendRef resolves to nothing "+
					"and requests matching that rule fail",
				route.Kind, route.Name, route.Namespace,
				ref.toKind, ref.toName, target,
				route.Kind, route.Namespace),
		})
	}
	return diagnostics
}
