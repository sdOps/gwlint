// Package danglingbackendref flags a route whose backendRefs name a Service or
// Envoy Gateway Backend that is not present. Gateway API resolves a backendRef
// at runtime; a name that resolves to nothing does not fail the apply, it
// makes the route serve 500s for that backend instead. This is the Gateway API
// counterpart of kube-linter's own dangling-service.
package danglingbackendref

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

const templateKey = "dangling-backend-ref"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling Route backendRef",
		Key:       templateKey,
		Description: "Flag routes whose backendRefs name a Service or Backend that is not present, " +
			"so requests matching that rule have nowhere to go",
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

// namespacedKind is a kind scoped to a namespace, which is the granularity at
// which the manifest set either describes something or does not.
type namespacedKind struct {
	kind      string
	namespace string
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return nil
	}

	known := gatewayapi.BackendTargets(lintCtx)
	// Judge a reference only into a namespace the manifest set actually
	// describes. Routes routinely point at workloads owned by another team and
	// rendered from another repo, and a namespace absent from the set says
	// nothing about what exists there. A weaker guard, that any object of the
	// kind exists anywhere, is not enough: a set can carry a Service in its own
	// namespace while saying nothing about the application namespaces its
	// routes target.
	described := map[namespacedKind]bool{}
	for ref := range known {
		described[namespacedKind{kind: ref.Kind, namespace: ref.Namespace}] = true
	}

	var diagnostics []diagnostic.Diagnostic
	for _, backendRef := range route.BackendRefs {
		group, kind := backendRef.Group(), backendRef.Kind()
		// A ref into some other vendor's CRD is not something gwlint can judge:
		// its absence here says nothing about whether it exists in a cluster.
		if !gatewayapi.ResolvableBackendKind(group, kind) {
			continue
		}
		namespace := backendRef.Namespace(route.Namespace)
		if !described[namespacedKind{kind: kind, namespace: namespace}] {
			continue
		}
		name := string(backendRef.Ref.Name)
		if _, found := known[gatewayapi.ObjectRef{Kind: kind, Namespace: namespace, Name: name}]; found {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q routes to %s %q in namespace %q, which is not present; "+
					"requests matching that rule have nowhere to go",
				route.Kind, route.Name, kind, name, namespace),
		})
	}
	return diagnostics
}
