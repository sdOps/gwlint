// Package routenotpermitted flags a route a listener will not admit, because
// the listener's allowedRoutes excludes the route's namespace or its kind. The
// route is valid, the Gateway is valid, and the attachment simply never
// happens; the reason appears only in the route's status conditions.
package routenotpermitted

import (
	"fmt"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "route-not-permitted"

func init() {
	templates.Register(check.Template{
		HumanName: "Listener Does Not Admit Route",
		Key:       templateKey,
		Description: "Flag routes a listener will not admit because its allowedRoutes excludes the route's " +
			"namespace or kind, so the route never attaches",
		SupportedObjectKinds: config.ObjectKindsDesc{ObjectKinds: gwobjectkinds.RouteKinds},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) { return checkFunc, nil },
	})
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return nil
	}
	byOwner := gatewayapi.ListenersByOwner(lintCtx)

	var diagnostics []diagnostic.Diagnostic
	for _, parent := range gatewayapi.ParentsOf(route) {
		if parent.Group != gatewayapi.GroupName {
			continue
		}
		listeners, present := byOwner[gatewayapi.ObjectRef{
			Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name,
		}]
		if !present {
			continue // dangling-parent-ref's finding, not this one.
		}
		candidates := listenersNamed(listeners, parent)
		if len(candidates) == 0 {
			continue // listener-not-found's finding.
		}
		// A route attaches if any candidate listener admits it. Only when every
		// one refuses does the attachment actually fail.
		if reason := whyNoneAdmit(candidates, route); reason != "" {
			diagnostics = append(diagnostics, diagnostic.Diagnostic{
				Message: fmt.Sprintf("%s %q attaches to %s %q, which will not admit it: %s; "+
					"the route never attaches and its traffic is not served",
					route.Kind, route.Name, parent.Kind, parent.Name, reason),
			})
		}
	}
	return diagnostics
}

func listenersNamed(listeners []gatewayapi.Listener, parent gatewayapi.Parent) []gatewayapi.Listener {
	if parent.Section == nil {
		return listeners
	}
	for _, l := range listeners {
		if l.Name == *parent.Section {
			return []gatewayapi.Listener{l}
		}
	}
	return nil
}

// whyNoneAdmit returns why every candidate listener refuses the route, or ""
// if any one of them admits it.
func whyNoneAdmit(listeners []gatewayapi.Listener, route gatewayapi.Route) string {
	var reasons []string
	for _, l := range listeners {
		if !l.AcceptsNamespace(route.Namespace) {
			reasons = append(reasons, fmt.Sprintf(
				"listener %q admits routes from %q namespaces and the route is in %q",
				l.Name, l.NamespacePolicy(), route.Namespace))
			continue
		}
		// A kind refused by the listener's protocol default, with no explicit
		// allowedRoutes.kinds narrowing it, is protocol-mismatch's finding.
		// Reporting it here too would say the same thing twice.
		if !l.HasExplicitKinds() {
			return ""
		}
		accepted, kinds := l.AcceptsKind(string(route.Kind))
		if !accepted {
			reasons = append(reasons, fmt.Sprintf(
				"listener %q accepts %s and the route is a %s",
				l.Name, strings.Join(kinds, " or "), route.Kind))
			continue
		}
		return "" // this listener admits it
	}
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, "; ")
}
