// Package gatewayservesnoroutes flags a Gateway that no route attaches to.
// Such a Gateway is still provisioned: the implementation creates the data
// plane and asks the cloud for an address, the object reports Programmed, and
// every request that reaches it falls through to nothing. Nothing in the API
// couples a Gateway to a route, so no schema validator can see this; the
// Gateway is perfectly valid on its own, and the missing half is a route in
// some other file that was renamed, deleted, or never written.
//
// The guard matters more here than in most checks. Gateways and routes are
// routinely split across repos, so a set with no routes in it at all is a
// platform repo being linted on its own, not a fleet of idle Gateways. Absent
// any route, every Gateway in the set would be flagged, which is noise rather
// than a finding, so the check stays quiet until the set carries at least one
// route to judge against. The guard is deliberately not narrowed to the
// Gateway's own namespace: routes attach across namespaces all the time, so a
// route anywhere in the set is evidence that routes were passed in.
package gatewayservesnoroutes

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "gateway-serves-no-routes"

func init() {
	templates.Register(check.Template{
		HumanName: "Gateway serves no routes",
		Key:       templateKey,
		Description: "Flag Gateways that no route in the manifest set attaches to, " +
			"so the Gateway is provisioned and serves no traffic",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.Gateway},
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
	gateway, ok := gatewayapi.AsGateway(object.K8sObject)
	if !ok {
		return nil
	}

	routes := gatewayapi.Routes(lintCtx)
	// See the package comment: no routes in the set means the routes were not
	// passed in, not that the Gateway has none.
	if len(routes) == 0 {
		return nil
	}

	self := gatewayapi.ObjectRef{
		Kind: gwobjectkinds.Gateway, Namespace: gateway.Namespace, Name: gateway.Name,
	}
	// A route reaches this Gateway either by naming it or by naming a
	// ListenerSet that attaches to it, so both are parentRefs that count.
	attachPoints := map[gatewayapi.ObjectRef]struct{}{self: {}}
	for ref := range listenerSetsOf(lintCtx, self) {
		attachPoints[ref] = struct{}{}
	}

	for _, route := range routes {
		for _, parent := range gatewayapi.ParentsOf(route) {
			// A parentRef into another API group is some other
			// implementation's attachment point, not this Gateway.
			if parent.Group != gatewayapi.GroupName {
				continue
			}
			ref := gatewayapi.ObjectRef{Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name}
			if _, found := attachPoints[ref]; found {
				return nil
			}
		}
	}

	return []diagnostic.Diagnostic{{
		Message: fmt.Sprintf(
			"Gateway %q in namespace %q has no route attached; no route in the manifest set names it "+
				"as a parentRef, directly or through a ListenerSet, so it is provisioned and serves no traffic",
			gateway.Name, gateway.Namespace),
	}}
}

// listenerSetsOf returns the ListenerSets in the context that attach to the
// given Gateway, which is the second way a route can reach it.
func listenerSetsOf(lintCtx lintcontext.LintContext, gateway gatewayapi.ObjectRef) map[gatewayapi.ObjectRef]struct{} {
	owned := map[gatewayapi.ObjectRef]struct{}{}
	for _, obj := range lintCtx.Objects() {
		listenerSet, ok := obj.K8sObject.(*gatewayv1.ListenerSet)
		if !ok {
			continue
		}
		if gatewayapi.ListenerSetParent(listenerSet) != gateway {
			continue
		}
		owned[gatewayapi.ObjectRef{
			Kind:      gwobjectkinds.ListenerSet,
			Namespace: listenerSet.Namespace,
			Name:      listenerSet.Name,
		}] = struct{}{}
	}
	return owned
}
