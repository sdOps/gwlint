package fqdnbackendcoldstart

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// resolvedBackendRef is one backendRef together with the name of the rule it
// came from, which a policy targetRef's sectionName can narrow the policy to.
type resolvedBackendRef struct {
	Rule *gatewayv1.SectionName
	Ref  gatewayv1.BackendRef
}

// resolvedRoute is a Gateway API route of any kind, reduced to what this check
// needs: how a policy can reach it, and what it routes to.
type resolvedRoute struct {
	Kind        gatewayv1.Kind
	Namespace   string
	Name        gatewayv1.ObjectName
	Labels      map[string]string
	ParentRefs  []gatewayv1.ParentReference
	BackendRefs []resolvedBackendRef
}

// asResolvedRoute extracts what the check needs from obj if it is any Gateway
// API route kind, at any served version, and reports whether it was one.
// v1beta1 and v1alpha3 route types are Go aliases of their v1 equivalents, so
// they convert directly; the v1alpha2 TCP/TLS/UDP types are their own structs
// but reuse v1's ParentReference and BackendRef, so their fields are read as-is.
func asResolvedRoute(obj k8sutil.Object) (resolvedRoute, bool) {
	switch route := obj.(type) {
	case *gatewayv1.HTTPRoute:
		return httpRoute(route), true
	case *gatewayv1b1.HTTPRoute:
		return httpRoute((*gatewayv1.HTTPRoute)(route)), true
	case *gatewayv1.GRPCRoute:
		return grpcRoute(route), true
	case *gatewayv1a2.GRPCRoute:
		return grpcRoute((*gatewayv1.GRPCRoute)(route)), true
	case *gatewayv1.TCPRoute:
		return tcpRoute(route), true
	case *gatewayv1a2.TCPRoute:
		return newResolvedRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, route.Spec.ParentRefs,
			alpha2TCPBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.TLSRoute:
		return tlsRoute(route), true
	case *gatewayv1a3.TLSRoute:
		return tlsRoute((*gatewayv1.TLSRoute)(route)), true
	case *gatewayv1a2.TLSRoute:
		return newResolvedRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.ParentRefs,
			alpha2TLSBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.UDPRoute:
		return udpRoute(route), true
	case *gatewayv1a2.UDPRoute:
		return newResolvedRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, route.Spec.ParentRefs,
			alpha2UDPBackendRefs(route.Spec.Rules)), true
	default:
		return resolvedRoute{}, false
	}
}

func httpRoute(route *gatewayv1.HTTPRoute) resolvedRoute {
	var refs []resolvedBackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, resolvedBackendRef{Rule: rule.Name, Ref: br.BackendRef})
		}
	}
	return newResolvedRoute(gwobjectkinds.HTTPRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func grpcRoute(route *gatewayv1.GRPCRoute) resolvedRoute {
	var refs []resolvedBackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, resolvedBackendRef{Rule: rule.Name, Ref: br.BackendRef})
		}
	}
	return newResolvedRoute(gwobjectkinds.GRPCRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func tcpRoute(route *gatewayv1.TCPRoute) resolvedRoute {
	var refs []resolvedBackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return newResolvedRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func tlsRoute(route *gatewayv1.TLSRoute) resolvedRoute {
	var refs []resolvedBackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return newResolvedRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func udpRoute(route *gatewayv1.UDPRoute) resolvedRoute {
	var refs []resolvedBackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return newResolvedRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

// The v1alpha2 rule types are their own structs rather than aliases, so they
// need their own extractors, even though the BackendRef inside each is v1's.
func alpha2TCPBackendRefs(rules []gatewayv1a2.TCPRouteRule) []resolvedBackendRef {
	var refs []resolvedBackendRef
	for _, rule := range rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func alpha2TLSBackendRefs(rules []gatewayv1a2.TLSRouteRule) []resolvedBackendRef {
	var refs []resolvedBackendRef
	for _, rule := range rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func alpha2UDPBackendRefs(rules []gatewayv1a2.UDPRouteRule) []resolvedBackendRef {
	var refs []resolvedBackendRef
	for _, rule := range rules {
		refs = append(refs, namedBackendRefs(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func namedBackendRefs(rule *gatewayv1.SectionName, backendRefs []gatewayv1.BackendRef) []resolvedBackendRef {
	refs := make([]resolvedBackendRef, 0, len(backendRefs))
	for _, br := range backendRefs {
		refs = append(refs, resolvedBackendRef{Rule: rule, Ref: br})
	}
	return refs
}

func newResolvedRoute(kind string, meta metav1.ObjectMeta, parentRefs []gatewayv1.ParentReference, backendRefs []resolvedBackendRef) resolvedRoute {
	return resolvedRoute{
		Kind:        gatewayv1.Kind(kind),
		Namespace:   meta.Namespace,
		Name:        gatewayv1.ObjectName(meta.Name),
		Labels:      meta.Labels,
		ParentRefs:  parentRefs,
		BackendRefs: backendRefs,
	}
}
