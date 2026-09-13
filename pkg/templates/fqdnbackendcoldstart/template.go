// Package fqdnbackendcoldstart flags BackendTrafficPolicies that leave a
// cold-start gap: no health check, covering a route whose backend resolves
// by FQDN. Envoy Gateway automatically puts FQDN-endpoint Backends on the
// same DNS-resolving cluster path the legacy STRICT_DNS Envoy cluster type
// used, so a request that arrives before the name resolves 503s, and with
// no health check there is nothing to route around it during that window.
// This mirrors a real incident: a cold-start DNS resolution gap caused 503s
// on FQDN-backed routes. See the README for the incident writeup and remediation.
package fqdnbackendcoldstart

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "fqdn-backend-cold-start"

func init() {
	templates.Register(check.Template{
		HumanName: "FQDN Backend Cold-Start Risk",
		Key:       templateKey,
		Description: "Flag BackendTrafficPolicies with no health check that cover a route whose backend " +
			"resolves via FQDN, which can 503 before DNS resolves at startup",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.BackendTrafficPolicy},
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
	policy, ok := object.K8sObject.(*egv1a1.BackendTrafficPolicy)
	if !ok {
		return nil
	}
	if hasHealthCheck(policy) {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, coverage := range policyCoverage(lintCtx, policy) {
		ref, route := coverage.ref, coverage.route
		for _, backendRef := range route.BackendRefs {
			if !isEnvoyGatewayBackendRef(backendRef) {
				continue
			}
			// Gateway API defaults an omitted backendRef namespace to the
			// namespace of the route doing the referring, which is not always
			// the policy's: a Gateway-level policy reaches routes in other
			// namespaces that attach to it via parentRefs.
			backendNamespace := route.Namespace
			if backendRef.Namespace != nil {
				backendNamespace = string(*backendRef.Namespace)
			}
			backend := findBackend(lintCtx, backendNamespace, string(backendRef.Name))
			if backend == nil {
				continue
			}
			if hostname, ok := fqdnHostname(backend); ok {
				diagnostics = append(diagnostics, diagnostic.Diagnostic{
					Message: fmt.Sprintf(
						"BackendTrafficPolicy has no health check, but %s Backend %q with FQDN "+
							"endpoint %q; this backend resolves via DNS at startup and can 503 "+
							"before the name resolves",
						describeCoverage(ref, route), backend.Name, hostname),
				})
			}
		}
	}
	return diagnostics
}

// coveredRoute pairs a route with the targetRef or selector that reached it,
// so the diagnostic can say how the policy gets there.
type coveredRoute struct {
	ref   targetRef
	route resolvedRoute
}

// policyCoverage returns every route this policy applies to, through its
// targetRef, its targetRefs, and its targetSelectors.
func policyCoverage(lintCtx lintcontext.LintContext, policy *egv1a1.BackendTrafficPolicy) []coveredRoute {
	var covered []coveredRoute
	for _, ref := range policyTargetRefs(policy) {
		for _, route := range resolveRoutes(lintCtx, policy.Namespace, ref) {
			covered = append(covered, coveredRoute{ref: ref, route: route})
		}
	}
	for _, sel := range policy.Spec.TargetSelectors {
		for _, route := range resolveSelectedRoutes(lintCtx, policy.Namespace, sel) {
			// A selector names no single object, so the diagnostic describes
			// the route it matched rather than a target.
			covered = append(covered, coveredRoute{
				ref:   targetRef{Kind: route.Kind, Name: route.Name, viaSelector: true},
				route: route,
			})
		}
	}
	return covered
}

func hasHealthCheck(policy *egv1a1.BackendTrafficPolicy) bool {
	hc := policy.Spec.HealthCheck
	return hc != nil && (hc.Active != nil || hc.Passive != nil)
}

// targetRef is a normalized view of BackendTrafficPolicy's deprecated single
// TargetRef and its current TargetRefs, which share the same Kind/Name shape.
type targetRef struct {
	Kind gatewayv1.Kind
	Name gatewayv1.ObjectName
	// viaSelector marks a route reached through targetSelectors rather than
	// named by a targetRef, which the diagnostic wording has to reflect.
	viaSelector bool
}

func policyTargetRefs(policy *egv1a1.BackendTrafficPolicy) []targetRef {
	var refs []targetRef
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		refs = append(refs, targetRef{Kind: ref.Kind, Name: ref.Name})
	}
	for _, r := range policy.Spec.TargetRefs {
		refs = append(refs, targetRef{Kind: r.Kind, Name: r.Name})
	}
	return refs
}

// resolvedRoute is an HTTPRoute or GRPCRoute this check has resolved a
// BackendTrafficPolicy's targetRef down to, along with the backendRefs to
// inspect for FQDN-endpoint Backends.
type resolvedRoute struct {
	Kind        gatewayv1.Kind
	Namespace   string
	Name        gatewayv1.ObjectName
	Labels      map[string]string
	ParentRefs  []gatewayv1.ParentReference
	BackendRefs []gatewayv1.BackendRef
}

// resolveRoutes returns the routes a BackendTrafficPolicy's targetRef
// actually covers. A route-kind targetRef resolves to that route directly. A
// Gateway-kind targetRef resolves to every route attached to that Gateway,
// whether attached to the Gateway itself or to a ListenerSet that belongs to
// it, since Gateway API's policy attachment hierarchy runs Gateway to
// ListenerSet to Route and a Gateway-level policy applies to everything under
// it by default. A ListenerSet targetRef resolves to the routes attached to
// that ListenerSet alone.
func resolveRoutes(lintCtx lintcontext.LintContext, policyNamespace string, ref targetRef) []resolvedRoute {
	switch string(ref.Kind) {
	case gwobjectkinds.Gateway:
		return routesAttachedToGateway(lintCtx, policyNamespace, string(ref.Name))
	case gwobjectkinds.ListenerSet:
		return routesAttachedTo(lintCtx, gwobjectkinds.ListenerSet, policyNamespace, string(ref.Name))
	default:
		if !isRouteKind(ref.Kind) {
			return nil
		}
		obj := findRoute(lintCtx, policyNamespace, string(ref.Name), ref.Kind)
		if obj == nil {
			return nil
		}
		return []resolvedRoute{*obj}
	}
}

func isRouteKind(kind gatewayv1.Kind) bool {
	for _, k := range gwobjectkinds.RouteKinds {
		if string(kind) == k {
			return true
		}
	}
	return false
}

// resolveSelectedRoutes returns the routes a policy's targetSelectors match.
// Envoy Gateway selects by kind plus labels, scoped to a set of namespaces.
func resolveSelectedRoutes(lintCtx lintcontext.LintContext, policyNamespace string, sel egv1a1.TargetSelector) []resolvedRoute {
	if sel.Group != nil && string(*sel.Group) != gatewayv1.GroupName {
		return nil
	}
	if !isRouteKind(sel.Kind) {
		return nil
	}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels:      sel.MatchLabels,
		MatchExpressions: sel.MatchExpressions,
	})
	if err != nil {
		// An unparseable selector selects nothing rather than everything.
		return nil
	}

	var routes []resolvedRoute
	for _, obj := range lintCtx.Objects() {
		route, ok := asResolvedRoute(obj.K8sObject)
		if !ok || route.Kind != sel.Kind {
			continue
		}
		if !namespaceSelected(sel.Namespaces, policyNamespace, route.Namespace) {
			continue
		}
		if !selector.Matches(labels.Set(route.Labels)) {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

// namespaceSelected reports whether a route's namespace is in scope for a
// target selector. "Same" (the default) means the policy's own namespace and
// "All" means any; a namespace label Selector is not resolved, since deciding
// it needs the Namespace objects, which are not part of a manifest set.
func namespaceSelected(ns *egv1a1.TargetSelectorNamespaces, policyNamespace, routeNamespace string) bool {
	if ns == nil || ns.From == egv1a1.TargetNamespaceFromSame {
		return routeNamespace == policyNamespace
	}
	return ns.From == egv1a1.TargetNamespaceFromAll
}

func findRoute(lintCtx lintcontext.LintContext, namespace, name string, kind gatewayv1.Kind) *resolvedRoute {
	for _, obj := range lintCtx.Objects() {
		if obj.K8sObject.GetNamespace() != namespace || obj.K8sObject.GetName() != name {
			continue
		}
		if route, ok := asResolvedRoute(obj.K8sObject); ok && route.Kind == kind {
			return &route
		}
	}
	return nil
}

// routesAttachedToGateway returns every route attached to the given Gateway,
// directly or through a ListenerSet whose parentRef names that Gateway.
func routesAttachedToGateway(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string) []resolvedRoute {
	routes := routesAttachedTo(lintCtx, gwobjectkinds.Gateway, gatewayNamespace, gatewayName)

	seen := make(map[routeKey]struct{}, len(routes))
	for _, r := range routes {
		seen[routeKey{r.Kind, r.Namespace, r.Name}] = struct{}{}
	}
	for _, ls := range listenerSetsOfGateway(lintCtx, gatewayNamespace, gatewayName) {
		for _, r := range routesAttachedTo(lintCtx, gwobjectkinds.ListenerSet, ls.Namespace, ls.Name) {
			key := routeKey{r.Kind, r.Namespace, r.Name}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			routes = append(routes, r)
		}
	}
	return routes
}

// routeKey identifies a route for de-duplication: a route can name both a
// Gateway and one of its ListenerSets in parentRefs.
type routeKey struct {
	kind      gatewayv1.Kind
	namespace string
	name      gatewayv1.ObjectName
}

// routesAttachedTo returns every route whose parentRefs name the given
// Gateway or ListenerSet.
func routesAttachedTo(lintCtx lintcontext.LintContext, parentKind, parentNamespace, parentName string) []resolvedRoute {
	var routes []resolvedRoute
	for _, obj := range lintCtx.Objects() {
		route, ok := asResolvedRoute(obj.K8sObject)
		if !ok {
			continue
		}
		if parentRefsMatch(route.ParentRefs, route.Namespace, parentKind, parentNamespace, parentName) {
			routes = append(routes, route)
		}
	}
	return routes
}

// listenerSetRef is a ListenerSet and the Gateway its parentRef names.
type listenerSetRef struct {
	Namespace string
	Name      string
}

func listenerSetsOfGateway(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string) []listenerSetRef {
	var sets []listenerSetRef
	for _, obj := range lintCtx.Objects() {
		ls, ok := obj.K8sObject.(*gatewayv1.ListenerSet)
		if !ok {
			continue
		}
		parent := ls.Spec.ParentRef
		if parent.Group != nil && string(*parent.Group) != gatewayv1.GroupName {
			continue
		}
		if parent.Kind != nil && string(*parent.Kind) != gwobjectkinds.Gateway {
			continue
		}
		ns := ls.Namespace
		if parent.Namespace != nil {
			ns = string(*parent.Namespace)
		}
		if ns == gatewayNamespace && string(parent.Name) == gatewayName {
			sets = append(sets, listenerSetRef{Namespace: ls.Namespace, Name: ls.Name})
		}
	}
	return sets
}

// parentRefsMatch reports whether any parentRef names the given parent. An
// omitted parentRef kind defaults to Gateway, and an omitted namespace to the
// route's own, both per Gateway API.
func parentRefsMatch(parentRefs []gatewayv1.ParentReference, routeNamespace, parentKind, parentNamespace, parentName string) bool {
	for _, p := range parentRefs {
		if p.Group != nil && string(*p.Group) != gatewayv1.GroupName {
			continue
		}
		kind := gwobjectkinds.Gateway
		if p.Kind != nil {
			kind = string(*p.Kind)
		}
		if kind != parentKind {
			continue
		}
		ns := routeNamespace
		if p.Namespace != nil {
			ns = string(*p.Namespace)
		}
		if ns == parentNamespace && string(p.Name) == parentName {
			return true
		}
	}
	return false
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
		return newResolvedRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, route.Spec.ParentRefs, alpha2TCPBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.TLSRoute:
		return tlsRoute(route), true
	case *gatewayv1a3.TLSRoute:
		return tlsRoute((*gatewayv1.TLSRoute)(route)), true
	case *gatewayv1a2.TLSRoute:
		return newResolvedRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.ParentRefs, alpha2TLSBackendRefs(route.Spec.Rules)), true
	case *gatewayv1.UDPRoute:
		return udpRoute(route), true
	case *gatewayv1a2.UDPRoute:
		return newResolvedRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, route.Spec.ParentRefs, alpha2UDPBackendRefs(route.Spec.Rules)), true
	default:
		return resolvedRoute{}, false
	}
}

func httpRoute(route *gatewayv1.HTTPRoute) resolvedRoute {
	var refs []gatewayv1.BackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, br.BackendRef)
		}
	}
	return newResolvedRoute(gwobjectkinds.HTTPRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func grpcRoute(route *gatewayv1.GRPCRoute) resolvedRoute {
	var refs []gatewayv1.BackendRef
	for _, rule := range route.Spec.Rules {
		for _, br := range rule.BackendRefs {
			refs = append(refs, br.BackendRef)
		}
	}
	return newResolvedRoute(gwobjectkinds.GRPCRoute, route.ObjectMeta, route.Spec.ParentRefs, refs)
}

func tcpRoute(route *gatewayv1.TCPRoute) resolvedRoute {
	return newResolvedRoute(gwobjectkinds.TCPRoute, route.ObjectMeta, route.Spec.ParentRefs, tcpBackendRefs(route.Spec.Rules))
}

func tlsRoute(route *gatewayv1.TLSRoute) resolvedRoute {
	return newResolvedRoute(gwobjectkinds.TLSRoute, route.ObjectMeta, route.Spec.ParentRefs, tlsBackendRefs(route.Spec.Rules))
}

func udpRoute(route *gatewayv1.UDPRoute) resolvedRoute {
	return newResolvedRoute(gwobjectkinds.UDPRoute, route.ObjectMeta, route.Spec.ParentRefs, udpBackendRefs(route.Spec.Rules))
}

func tcpBackendRefs(rules []gatewayv1.TCPRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

func tlsBackendRefs(rules []gatewayv1.TLSRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

func udpBackendRefs(rules []gatewayv1.UDPRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

// The v1alpha2 rule types are their own structs rather than aliases, so they
// need their own extractors, even though the BackendRef inside each is v1's.
func alpha2TCPBackendRefs(rules []gatewayv1a2.TCPRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

func alpha2TLSBackendRefs(rules []gatewayv1a2.TLSRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

func alpha2UDPBackendRefs(rules []gatewayv1a2.UDPRouteRule) []gatewayv1.BackendRef {
	var refs []gatewayv1.BackendRef
	for _, rule := range rules {
		refs = append(refs, rule.BackendRefs...)
	}
	return refs
}

func newResolvedRoute(kind string, meta metav1.ObjectMeta, parentRefs []gatewayv1.ParentReference, backendRefs []gatewayv1.BackendRef) resolvedRoute {
	return resolvedRoute{
		Kind:        gatewayv1.Kind(kind),
		Namespace:   meta.Namespace,
		Name:        gatewayv1.ObjectName(meta.Name),
		Labels:      meta.Labels,
		ParentRefs:  parentRefs,
		BackendRefs: backendRefs,
	}
}

// describeCoverage renders how the policy reaches the FQDN-backed route, as
// the clause leading into the Backend name in the diagnostic message. For a
// Gateway-kind targetRef the route carrying the FQDN backend is named too,
// since the policy itself never mentions it.
func describeCoverage(ref targetRef, route resolvedRoute) string {
	switch {
	case ref.viaSelector:
		return fmt.Sprintf("selects %s %q, which routes to", route.Kind, route.Name)
	case string(ref.Kind) == gwobjectkinds.Gateway, string(ref.Kind) == gwobjectkinds.ListenerSet:
		return fmt.Sprintf("targets %s %q, whose attached %s %q routes to", ref.Kind, ref.Name, route.Kind, route.Name)
	default:
		return fmt.Sprintf("targets %s %q, which routes to", ref.Kind, ref.Name)
	}
}

// isEnvoyGatewayBackendRef reports whether ref points at an Envoy Gateway
// Backend object, as opposed to the default (a core Service).
func isEnvoyGatewayBackendRef(ref gatewayv1.BackendRef) bool {
	if ref.Kind == nil || string(*ref.Kind) != egv1a1.KindBackend {
		return false
	}
	return ref.Group != nil && string(*ref.Group) == egv1a1.GroupName
}

func findBackend(lintCtx lintcontext.LintContext, namespace, name string) *egv1a1.Backend {
	for _, obj := range lintCtx.Objects() {
		backend, ok := obj.K8sObject.(*egv1a1.Backend)
		if !ok {
			continue
		}
		if backend.Namespace == namespace && backend.Name == name {
			return backend
		}
	}
	return nil
}

func fqdnHostname(backend *egv1a1.Backend) (string, bool) {
	for _, endpoint := range backend.Spec.Endpoints {
		if endpoint.FQDN != nil && endpoint.FQDN.Hostname != "" {
			return endpoint.FQDN.Hostname, true
		}
	}
	return "", false
}
