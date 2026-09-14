// Package protocolmismatch flags a route attached to a listener whose protocol
// cannot carry it: an HTTPRoute on a TCP listener, a TLSRoute on an HTTP one.
// Gateway API derives the route kinds a listener accepts from its protocol,
// and a route of any other kind is never programmed onto it. Both objects are
// individually valid, the apply succeeds, and the reason lives in a status
// condition nobody reads, which is why a schema validator cannot see it: the
// two fields are fine on their own and only disagree with each other.
//
// The check stays quiet where another check owns the finding. A parentRef
// whose parent is absent selects no listeners, which is dangling-parent-ref's
// finding and is also what keeps this quiet on a routes-only manifest set,
// where the Gateway lives in another chart or repo; an unresolved sectionName
// is listener-not-found's; a listener refusing the route's namespace, or
// excluding its kind through an explicit allowedRoutes.kinds, is
// route-not-permitted's.
package protocolmismatch

import (
	"fmt"
	"slices"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "protocol-mismatch"

func init() {
	templates.Register(check.Template{
		HumanName: "Route Protocol Mismatch",
		Key:       templateKey,
		Description: "Flag routes attached to a listener whose protocol cannot carry their kind, " +
			"such as an HTTPRoute on a TCP listener, so the route is never programmed",
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
	byOwner := gatewayapi.ListenersByOwner(lintCtx)

	var diagnostics []diagnostic.Diagnostic
	for _, parent := range gatewayapi.ParentsOf(route) {
		// A parentRef into another API group is some other implementation's
		// attachment point, whose protocols gwlint knows nothing about.
		if parent.Group != gatewayapi.GroupName {
			continue
		}
		reason := whyNoListenerCarries(gatewayapi.ListenersFor(byOwner, parent), route)
		if reason == "" {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q attaches to %s %q, whose protocol cannot carry it: %s; "+
					"the route is never programmed and its traffic is not served",
				route.Kind, route.Name, parent.Kind, parent.Name, reason),
		})
	}
	return diagnostics
}

// whyNoListenerCarries returns why no listener the parentRef selects can carry
// the route's kind, or "" when one of them can, or when the refusal is another
// check's finding. A route attaches to every listener the parentRef selects,
// so one that can carry it is enough.
func whyNoListenerCarries(listeners []gatewayapi.Listener, route gatewayapi.Route) string {
	kind := string(route.Kind)

	var reasons []string
	for _, l := range listeners {
		// A listener that refuses the route's namespace refuses it for a reason
		// that has nothing to do with protocol; route-not-permitted covers that.
		if !l.AcceptsNamespace(route.Namespace) {
			continue
		}
		carried := gatewayapi.RouteKindsFor(l.Protocol)
		// An implementation-specific protocol carries whatever its
		// implementation says it does, which gwlint cannot know.
		if len(carried) == 0 || slices.Contains(carried, kind) {
			return ""
		}
		if setsRouteKinds(l) {
			if accepted, _ := l.AcceptsKind(kind); !accepted {
				// The listener's own allowedRoutes.kinds already excludes the
				// route, which route-not-permitted reports. Saying it twice in
				// two vocabularies helps nobody.
				continue
			}
			// allowedRoutes.kinds names a kind the protocol cannot carry, so the
			// listener advertises something it can never serve.
			reasons = append(reasons, fmt.Sprintf(
				"listener %q allows %s but speaks %s, which carries %s",
				l.Name, kind, l.Protocol, strings.Join(carried, " and ")))
			continue
		}
		reasons = append(reasons, fmt.Sprintf(
			"listener %q speaks %s, which carries %s",
			l.Name, l.Protocol, strings.Join(carried, " and ")))
	}
	if len(reasons) == 0 {
		return ""
	}
	return strings.Join(reasons, "; ")
}

// setsRouteKinds reports whether the listener names route kinds of its own,
// which replaces the protocol-derived default entirely.
func setsRouteKinds(l gatewayapi.Listener) bool {
	return l.Allowed != nil && len(l.Allowed.Kinds) > 0
}
