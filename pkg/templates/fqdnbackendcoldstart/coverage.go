package fqdnbackendcoldstart

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// targetRef is a normalized view of BackendTrafficPolicy's deprecated single
// TargetRef and its current TargetRefs, which share the same shape.
type targetRef struct {
	Kind        gatewayv1.Kind
	Name        gatewayv1.ObjectName
	SectionName *gatewayv1.SectionName
	// viaSelector marks a route reached through targetSelectors rather than
	// named by a targetRef, which the diagnostic wording has to reflect.
	viaSelector bool
}

func policyTargetRefs(policy *egv1a1.BackendTrafficPolicy) []targetRef {
	var refs []targetRef
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		refs = append(refs, targetRef{Kind: ref.Kind, Name: ref.Name, SectionName: ref.SectionName})
	}
	for _, r := range policy.Spec.TargetRefs {
		refs = append(refs, targetRef{Kind: r.Kind, Name: r.Name, SectionName: r.SectionName})
	}
	return refs
}

// coveredRoute pairs a route with the targetRef or selector that reached it,
// so the diagnostic can say how the policy gets there.
type coveredRoute struct {
	ref   targetRef
	route resolvedRoute
	// inherited marks coverage that came down the attachment hierarchy from a
	// Gateway or ListenerSet rather than naming the route. Gateway API lets a
	// more specific policy override an inherited one, so only inherited
	// coverage can be displaced.
	inherited bool
	// ruleSection narrows coverage to one named route rule. A targetRef's
	// sectionName means different things by target kind: a listener on a
	// Gateway or ListenerSet, which parentRefsMatch has already applied, and a
	// route rule on a route, which only this carries.
	ruleSection *gatewayv1.SectionName
}

// policyCoverage returns every route this policy applies to, through its
// targetRef, its targetRefs, and its targetSelectors.
func policyCoverage(lintCtx lintcontext.LintContext, policy *egv1a1.BackendTrafficPolicy) []coveredRoute {
	var covered []coveredRoute
	for _, ref := range policyTargetRefs(policy) {
		inherited := isParentKind(ref.Kind)
		// On a parent target the sectionName names a listener, not a rule.
		var ruleSection *gatewayv1.SectionName
		if !inherited {
			ruleSection = ref.SectionName
		}
		for _, route := range resolveRoutes(lintCtx, policy.Namespace, ref) {
			covered = append(covered, coveredRoute{
				ref:         ref,
				route:       route,
				inherited:   inherited,
				ruleSection: ruleSection,
			})
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

func isParentKind(kind gatewayv1.Kind) bool {
	return string(kind) == gwobjectkinds.Gateway || string(kind) == gwobjectkinds.ListenerSet
}

func isRouteKind(kind gatewayv1.Kind) bool {
	for _, k := range gwobjectkinds.RouteKinds {
		if string(kind) == k {
			return true
		}
	}
	return false
}

// resolveRoutes returns the routes a BackendTrafficPolicy's targetRef actually
// covers. A route-kind targetRef resolves to that route directly. A Gateway
// targetRef resolves to every route attached to that Gateway, whether attached
// to the Gateway itself or to a ListenerSet belonging to it, since Gateway
// API's attachment hierarchy runs Gateway to ListenerSet to Route and a
// Gateway-level policy applies to everything beneath it by default. A
// sectionName on a Gateway targetRef narrows that to one listener.
func resolveRoutes(lintCtx lintcontext.LintContext, policyNamespace string, ref targetRef) []resolvedRoute {
	switch string(ref.Kind) {
	case gwobjectkinds.Gateway:
		return routesAttachedToGateway(lintCtx, policyNamespace, string(ref.Name), ref.SectionName)
	case gwobjectkinds.ListenerSet:
		return routesAttachedTo(lintCtx, gwobjectkinds.ListenerSet, policyNamespace, string(ref.Name), ref.SectionName)
	default:
		if !isRouteKind(ref.Kind) {
			return nil
		}
		route := findRoute(lintCtx, policyNamespace, string(ref.Name), ref.Kind)
		if route == nil {
			return nil
		}
		return []resolvedRoute{*route}
	}
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
// it needs the Namespace objects, which a rendered manifest set rarely carries.
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
// directly or through a ListenerSet whose parentRef names that Gateway. A
// non-nil listener narrows it to routes attached to that listener.
func routesAttachedToGateway(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string, listener *gatewayv1.SectionName) []resolvedRoute {
	routes := routesAttachedTo(lintCtx, gwobjectkinds.Gateway, gatewayNamespace, gatewayName, listener)

	seen := make(map[routeKey]struct{}, len(routes))
	for _, r := range routes {
		seen[keyOf(r)] = struct{}{}
	}
	for _, ls := range listenerSetsOfGateway(lintCtx, gatewayNamespace, gatewayName) {
		// A listener named on the Gateway targetRef belongs to the Gateway, not
		// to the ListenerSet, so it does not narrow the ListenerSet's routes.
		for _, r := range routesAttachedTo(lintCtx, gwobjectkinds.ListenerSet, ls.Namespace, ls.Name, nil) {
			if _, dup := seen[keyOf(r)]; dup {
				continue
			}
			seen[keyOf(r)] = struct{}{}
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

func keyOf(r resolvedRoute) routeKey {
	return routeKey{kind: r.Kind, namespace: r.Namespace, name: r.Name}
}

// routesAttachedTo returns every route whose parentRefs name the given Gateway
// or ListenerSet, optionally narrowed to one listener section.
func routesAttachedTo(lintCtx lintcontext.LintContext, parentKind, parentNamespace, parentName string, listener *gatewayv1.SectionName) []resolvedRoute {
	var routes []resolvedRoute
	for _, obj := range lintCtx.Objects() {
		route, ok := asResolvedRoute(obj.K8sObject)
		if !ok {
			continue
		}
		if parentRefsMatch(route.ParentRefs, route.Namespace, parentKind, parentNamespace, parentName, listener) {
			routes = append(routes, route)
		}
	}
	return routes
}

// listenerSetRef locates a ListenerSet belonging to a Gateway.
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
// omitted parentRef kind defaults to Gateway and an omitted namespace to the
// route's own, both per Gateway API. When listener is non-nil the policy only
// covers that listener, and a parentRef matches if it names the same listener
// or names none at all, which attaches the route to every listener.
func parentRefsMatch(parentRefs []gatewayv1.ParentReference, routeNamespace, parentKind, parentNamespace, parentName string, listener *gatewayv1.SectionName) bool {
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
		if ns != parentNamespace || string(p.Name) != parentName {
			continue
		}
		if listener != nil && p.SectionName != nil && *p.SectionName != *listener {
			continue
		}
		return true
	}
	return false
}

// sectionCoversRule reports whether a policy targetRef's sectionName covers a
// given route rule. No sectionName covers the whole route; a sectionName names
// one rule, and a rule with no name of its own cannot be the one named.
func sectionCoversRule(section, rule *gatewayv1.SectionName) bool {
	if section == nil {
		return true
	}
	return rule != nil && *rule == *section
}

// isOverridden reports whether a more specific BackendTrafficPolicy displaces
// an inherited one for this route rule. Envoy Gateway's own field
// documentation is explicit that with no mergeType set, "only the most
// specific configuration takes effect", so a route-level policy that does
// configure a health check closes the gap a Gateway-level one leaves open, and
// reporting the Gateway-level policy for that route would be a false positive.
func isOverridden(lintCtx lintcontext.LintContext, route resolvedRoute, rule *gatewayv1.SectionName, self *egv1a1.BackendTrafficPolicy) bool {
	for _, obj := range lintCtx.Objects() {
		other, ok := obj.K8sObject.(*egv1a1.BackendTrafficPolicy)
		if !ok || other == self || !hasHealthCheck(other) {
			continue
		}
		for _, covered := range policyCoverage(lintCtx, other) {
			if covered.inherited || keyOf(covered.route) != keyOf(route) {
				continue
			}
			if sectionCoversRule(covered.ruleSection, rule) {
				return true
			}
		}
	}
	return false
}
