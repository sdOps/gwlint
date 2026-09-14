// Package servicebackendwithoutport flags a backendRef to a core Service that
// sets no port. Gateway API requires one: a Service is a set of ports, and
// without the number the implementation has nothing to forward to, so the
// reference does not resolve and the rule serves errors.
//
// A schema validator misses this because the requirement is a CEL rule on
// BackendObjectReference, "Must have port for Service reference", and not part
// of the structural schema. Offline validators such as kubeconform do not
// evaluate CEL, so the manifest passes there; the rule also leans on the
// API server having defaulted group to "" and kind to Service first, which an
// offline validator does not do either.
//
// Only Service backendRefs are judged. An Envoy Gateway Backend carries its
// own endpoint ports, so a ref to one needs no port, and a ref into some other
// vendor's CRD is not something gwlint can say anything about. The check reads
// one route in isolation and resolves no references, so it never fires because
// the Service happens to live in another repo: the port is missing or present
// on the route itself.
package servicebackendwithoutport

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

const templateKey = "service-backend-without-port"

func init() {
	templates.Register(check.Template{
		HumanName: "Service backendRef without a port",
		Key:       templateKey,
		Description: "Flag routes whose backendRefs name a core Service with no port, which Gateway API " +
			"requires, so the reference does not resolve",
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

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, backendRef := range route.BackendRefs {
		// An omitted group means core and an omitted kind means Service, which
		// is how most Service backendRefs are written.
		if backendRef.Group() != "" || backendRef.Kind() != "Service" {
			continue
		}
		if backendRef.Ref.Port != nil {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q routes to Service %q in namespace %q with no port; Gateway API requires a port "+
					"on a Service backendRef, so the reference does not resolve and the rule serves errors",
				route.Kind, route.Name, backendRef.Ref.Name, backendRef.Namespace(route.Namespace)),
		})
	}
	return diagnostics
}
