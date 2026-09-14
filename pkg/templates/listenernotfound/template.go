// Package listenernotfound flags a route whose parentRef names a listener the
// Gateway does not declare. Gateway API treats sectionName as a hard
// requirement: if it does not resolve, the route is not accepted onto that
// parent at all. Nothing about the manifest is invalid, the apply succeeds,
// and the reason lives in a status condition nobody reads.
package listenernotfound

import (
	"fmt"
	"sort"
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

const templateKey = "listener-not-found"

func init() {
	templates.Register(check.Template{
		HumanName: "Route Names a Missing Listener",
		Key:       templateKey,
		Description: "Flag routes whose parentRefs sectionName names a listener the Gateway does not declare, " +
			"so the route is never accepted onto it",
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
		// No sectionName attaches the route to every listener, so there is
		// nothing to resolve.
		if parent.Section == nil || parent.Group != gatewayapi.GroupName {
			continue
		}
		owner := gatewayapi.ObjectRef{Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name}
		listeners, present := byOwner[owner]
		// A parent that is not in the manifest set is dangling-parent-ref's
		// finding, not this one. Reporting both would say the same thing twice.
		if !present {
			continue
		}
		if hasListener(listeners, *parent.Section) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q attaches to listener %q on %s %q, which declares no such listener (it has %s); "+
					"the route is not accepted onto that parent and its traffic is not served",
				route.Kind, route.Name, *parent.Section, parent.Kind, parent.Name, describe(listeners)),
		})
	}
	return diagnostics
}

func hasListener(listeners []gatewayapi.Listener, name gatewayv1.SectionName) bool {
	for _, l := range listeners {
		if l.Name == name {
			return true
		}
	}
	return false
}

// describe names the listeners that do exist, which is almost always enough to
// spot the typo without opening the Gateway.
func describe(listeners []gatewayapi.Listener) string {
	if len(listeners) == 0 {
		return "no listeners"
	}
	names := make([]string, 0, len(listeners))
	for _, l := range listeners {
		names = append(names, fmt.Sprintf("%q", l.Name))
	}
	sort.Strings(names)
	if len(names) == 1 {
		return "only " + names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
