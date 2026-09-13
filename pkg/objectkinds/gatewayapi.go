package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// The Gateway API kinds gwlint understands. Each is matched on group and kind
// across every served version rather than pinned to one: TCPRoute, TLSRoute
// and UDPRoute are experimental-channel resources commonly authored as
// v1alpha2, ReferenceGrant as v1beta1, and a check should see the object
// whichever version it was written in. kube-linter's own object kinds match
// across versions the same way (see its HorizontalPodAutoscaler).
const (
	// Gateway represents Gateway API Gateway objects.
	Gateway = "Gateway"
	// ListenerSet represents Gateway API ListenerSet objects.
	ListenerSet = "ListenerSet"
	// HTTPRoute represents Gateway API HTTPRoute objects.
	HTTPRoute = "HTTPRoute"
	// GRPCRoute represents Gateway API GRPCRoute objects.
	GRPCRoute = "GRPCRoute"
	// TCPRoute represents Gateway API TCPRoute objects.
	TCPRoute = "TCPRoute"
	// TLSRoute represents Gateway API TLSRoute objects.
	TLSRoute = "TLSRoute"
	// UDPRoute represents Gateway API UDPRoute objects.
	UDPRoute = "UDPRoute"
	// ReferenceGrant represents Gateway API ReferenceGrant objects.
	ReferenceGrant = "ReferenceGrant"
)

// RouteKinds are the Gateway API route kinds that carry backendRefs, in the
// order Envoy Gateway lists them as valid policy targets.
var RouteKinds = []string{HTTPRoute, GRPCRoute, TCPRoute, TLSRoute, UDPRoute}

func init() {
	for _, kind := range []string{
		Gateway, ListenerSet, HTTPRoute, GRPCRoute, TCPRoute, TLSRoute, UDPRoute, ReferenceGrant,
	} {
		objectkinds.RegisterObjectKind(kind, gatewayAPIMatcher(kind))
	}
}

// gatewayAPIMatcher matches a kind in the Gateway API group at any version.
func gatewayAPIMatcher(kind string) objectkinds.Matcher {
	return objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk.Group == gatewayv1.GroupName && gvk.Kind == kind
	})
}
