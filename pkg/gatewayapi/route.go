// Package gatewayapi reduces Gateway API objects to the shape gwlint's checks
// work on. Every route kind carries parentRefs and backendRefs in a slightly
// different Go type, at several served API versions, and more than one check
// needs to walk them, so the flattening lives here rather than in each check.
package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// BackendRef is one backendRef together with the name of the rule it came
// from, which a policy targetRef's sectionName can narrow a policy to.
type BackendRef struct {
	Rule *gatewayv1.SectionName
	Ref  gatewayv1.BackendRef
}

// Namespace returns the namespace the ref resolves in. Gateway API defaults an
// omitted backendRef namespace to the namespace of the route doing the
// referring, which is not always the namespace of whatever is inspecting it.
func (b BackendRef) Namespace(routeNamespace string) string {
	if b.Ref.Namespace != nil {
		return string(*b.Ref.Namespace)
	}
	return routeNamespace
}

// Group returns the ref's API group, defaulting to core, which is what an
// omitted group means in Gateway API.
func (b BackendRef) Group() string {
	if b.Ref.Group == nil {
		return ""
	}
	return string(*b.Ref.Group)
}

// Kind returns the ref's kind, defaulting to Service per Gateway API.
func (b BackendRef) Kind() string {
	if b.Ref.Kind == nil {
		return "Service"
	}
	return string(*b.Ref.Kind)
}

// Route is any Gateway API route kind, at any served version, reduced to what
// the checks need.
type Route struct {
	Kind        gatewayv1.Kind
	Namespace   string
	Name        gatewayv1.ObjectName
	Labels      map[string]string
	Hostnames   []gatewayv1.Hostname
	ParentRefs  []gatewayv1.ParentReference
	BackendRefs []BackendRef
}

// Routes returns every route in the context, in the order the context lists
// them. Callers that need a stable order must sort.
func Routes(lintCtx lintcontext.LintContext) []Route {
	var routes []Route
	for _, obj := range lintCtx.Objects() {
		if route, ok := AsRoute(obj.K8sObject); ok {
			routes = append(routes, route)
		}
	}
	return routes
}

// AsRoute extracts a Route from obj if it is any Gateway API route kind, and
// reports whether it was one. v1beta1 and v1alpha3 route types are Go aliases
// of their v1 equivalents, so they convert directly; the v1alpha2 TCP, TLS and
// UDP types are their own structs but reuse v1's ParentReference and
// BackendRef, so their fields are read as they are.
func AsRoute(obj k8sutil.Object) (Route, bool) {
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
		return newRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, nil, route.Spec.ParentRefs,
			alpha2TCPBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.TLSRoute:
		return tlsRoute(route), true
	case *gatewayv1a3.TLSRoute:
		return tlsRoute((*gatewayv1.TLSRoute)(route)), true
	case *gatewayv1a2.TLSRoute:
		return newRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.Hostnames, route.Spec.ParentRefs,
			alpha2TLSBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.UDPRoute:
		return udpRoute(route), true
	case *gatewayv1a2.UDPRoute:
		return newRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, nil, route.Spec.ParentRefs,
			alpha2UDPBackendRefs(route.Spec.Rules)), true
	default:
		return Route{}, false
	}
}

func httpRoute(route *gatewayv1.HTTPRoute) Route {
	var refs []BackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, BackendRef{Rule: rule.Name, Ref: br.BackendRef})
		}
	}
	return newRoute(gwobjectkinds.HTTPRoute, route.ObjectMeta, route.Spec.Hostnames, route.Spec.ParentRefs, refs)
}

func grpcRoute(route *gatewayv1.GRPCRoute) Route {
	var refs []BackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, BackendRef{Rule: rule.Name, Ref: br.BackendRef})
		}
	}
	return newRoute(gwobjectkinds.GRPCRoute, route.ObjectMeta, route.Spec.Hostnames, route.Spec.ParentRefs, refs)
}

func tcpRoute(route *gatewayv1.TCPRoute) Route {
	var refs []BackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return newRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, nil, route.Spec.ParentRefs, refs)
}

func tlsRoute(route *gatewayv1.TLSRoute) Route {
	var refs []BackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return newRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.Hostnames, route.Spec.ParentRefs, refs)
}

func udpRoute(route *gatewayv1.UDPRoute) Route {
	var refs []BackendRef
	for _, rule := range route.Spec.Rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return newRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, nil, route.Spec.ParentRefs, refs)
}

// The v1alpha2 rule types are their own structs rather than aliases, so they
// need their own extractors, even though the BackendRef inside each is v1's.
func alpha2TCPBackendRefs(rules []gatewayv1a2.TCPRouteRule) []BackendRef {
	var refs []BackendRef
	for _, rule := range rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func alpha2TLSBackendRefs(rules []gatewayv1a2.TLSRouteRule) []BackendRef {
	var refs []BackendRef
	for _, rule := range rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func alpha2UDPBackendRefs(rules []gatewayv1a2.UDPRouteRule) []BackendRef {
	var refs []BackendRef
	for _, rule := range rules {
		refs = append(refs, named(rule.Name, rule.BackendRefs)...)
	}
	return refs
}

func named(rule *gatewayv1.SectionName, backendRefs []gatewayv1.BackendRef) []BackendRef {
	refs := make([]BackendRef, 0, len(backendRefs))
	for _, br := range backendRefs {
		refs = append(refs, BackendRef{Rule: rule, Ref: br})
	}
	return refs
}

func newRoute(kind string, meta metav1.ObjectMeta, hostnames []gatewayv1.Hostname, parentRefs []gatewayv1.ParentReference, backendRefs []BackendRef) Route {
	return Route{
		Kind:        gatewayv1.Kind(kind),
		Namespace:   meta.Namespace,
		Name:        gatewayv1.ObjectName(meta.Name),
		Labels:      meta.Labels,
		Hostnames:   hostnames,
		ParentRefs:  parentRefs,
		BackendRefs: backendRefs,
	}
}
