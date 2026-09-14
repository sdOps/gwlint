// Package hostnamenevermatches flags a route whose hostnames cannot intersect
// the hostname of any listener it attaches to. Gateway API intersects the two
// sets and programs the result: when the intersection is empty the route still
// attaches, the apply still succeeds, and no request can ever match it. A
// schema validator sees two well-formed hostname fields and has no reason to
// compare them, so this survives every structural check and shows up as a 404
// in production.
//
// Two guards keep it quiet on a partial manifest set. A parentRef whose
// Gateway or ListenerSet is absent selects no listeners, so linting routes
// without the chart that defines the Gateway reports nothing, and neither does
// a sectionName that resolves to no listener; those belong to
// dangling-parent-ref and listener-not-found. A listener that would refuse the
// route on namespace or kind is skipped too, since route-not-permitted and
// protocol-mismatch already say why that attachment fails.
package hostnamenevermatches

import (
	"fmt"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "hostname-never-matches"

func init() {
	templates.Register(check.Template{
		HumanName: "Route Hostnames Never Match the Listener",
		Key:       templateKey,
		Description: "Flag routes whose hostnames cannot intersect the hostname of any listener they attach to, " +
			"so no request can ever match the route",
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
	// A route with no hostnames of its own takes whatever the listener offers,
	// so there is nothing that can fail to intersect. That covers TCPRoute and
	// UDPRoute, which carry no hostnames at all.
	if !ok || len(route.Hostnames) == 0 {
		return nil
	}
	byOwner := gatewayapi.ListenersByOwner(lintCtx)

	var diagnostics []diagnostic.Diagnostic
	for _, parent := range gatewayapi.ParentsOf(route) {
		// A parentRef into another API group is some other implementation's
		// attachment point and not ours to resolve.
		if parent.Group != gatewayapi.GroupName {
			continue
		}
		offered := whyNoListenerMatches(gatewayapi.ListenersFor(byOwner, parent), route)
		if offered == "" {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q asks for %s on %s %q, where %s; the hostnames cannot intersect, "+
					"so no request can ever match the route",
				route.Kind, route.Name, hostnameList(route.Hostnames), parent.Kind, parent.Name, offered),
		})
	}
	return diagnostics
}

// whyNoListenerMatches describes the hostnames every listener the parentRef
// selects offers, or "" when one of them can match the route. A route attaches
// to every listener the parentRef selects, so one that intersects is enough.
func whyNoListenerMatches(listeners []gatewayapi.Listener, route gatewayapi.Route) string {
	var offered []string
	for _, l := range listeners {
		// A listener that would not admit the route at all fails for a reason
		// that is not about hostnames, and another check reports that one.
		if !l.AcceptsNamespace(route.Namespace) {
			continue
		}
		if accepted, _ := l.AcceptsKind(string(route.Kind)); !accepted {
			continue
		}
		if l.HostnamesIntersect(route.Hostnames) {
			return ""
		}
		// HostnamesIntersect only fails on a listener that set a hostname.
		offered = append(offered, fmt.Sprintf("listener %q serves %q", l.Name, *l.Hostname))
	}
	if len(offered) == 0 {
		return ""
	}
	return strings.Join(offered, " and ")
}

func hostnameList(hostnames []gatewayv1.Hostname) string {
	quoted := make([]string, 0, len(hostnames))
	for _, h := range hostnames {
		quoted = append(quoted, fmt.Sprintf("%q", h))
	}
	if len(quoted) == 1 {
		return "hostname " + quoted[0]
	}
	return "hostnames " + strings.Join(quoted, ", ")
}
