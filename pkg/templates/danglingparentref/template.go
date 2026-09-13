// Package danglingparentref flags a route whose parentRefs name a Gateway or
// ListenerSet that is not present. A route attaches to a Gateway by naming it,
// and nothing rejects a name that does not exist: the route is simply never
// programmed, and its traffic goes nowhere. The manifests stay valid and the
// apply succeeds, which is what makes it worth a linter.
package danglingparentref

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

const templateKey = "dangling-parent-ref"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling Route parentRef",
		Key:       templateKey,
		Description: "Flag routes whose parentRefs name a Gateway or ListenerSet that is not present, " +
			"so the route never attaches and its traffic is never served",
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

	known := gatewayapi.GatewaysAndListenerSets(lintCtx)
	// Linting a routes-only manifest set is a normal thing to do: the Gateway
	// usually lives in a different chart or repo. Absent any Gateway at all,
	// every parentRef would look dangling, which would be noise rather than a
	// finding. Only judge parentRefs once there is something to judge against.
	if len(known) == 0 {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, parent := range gatewayapi.ParentsOf(route) {
		// A parentRef into another API group is some other implementation's
		// attachment point and not ours to resolve.
		if parent.Group != gatewayapi.GroupName {
			continue
		}
		if _, found := known[gatewayapi.ObjectRef{
			Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name,
		}]; found {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q attaches to %s %q in namespace %q, which is not present; "+
					"the route will never be programmed and its traffic will not be served",
				route.Kind, route.Name, parent.Kind, parent.Name, parent.Namespace),
		})
	}
	return diagnostics
}
