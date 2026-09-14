package gatewayapi

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// Working out which routes an Envoy Gateway policy applies to is not a lookup.
// A policy reaches routes by naming them, by naming a Gateway or ListenerSet
// they attach to, or by selecting them with labels, and a sectionName means a
// listener on a parent but a rule on a route. More than one check needs that
// answer, so the resolution lives here rather than in a check package.

// PolicyTarget is a normalized view of how a policy reaches an object: through
// its deprecated single targetRef, one of its targetRefs, or a targetSelector.
type PolicyTarget struct {
	Kind        gatewayv1.Kind
	Name        gatewayv1.ObjectName
	SectionName *gatewayv1.SectionName
	// ViaSelector marks a route reached through targetSelectors rather than
	// named by a targetRef, which diagnostic wording has to reflect: a
	// selector names no single object to point the reader at.
	ViaSelector bool
}

// CoveredRoute is one route a policy applies to, with how the policy got there.
type CoveredRoute struct {
	Target PolicyTarget
	Route  Route
	// Inherited marks coverage that came down the attachment hierarchy from a
	// Gateway or ListenerSet rather than naming the route. Gateway API lets a
	// more specific policy override an inherited one, so only inherited
	// coverage can be displaced.
	Inherited bool
	// RuleSection narrows coverage to one named route rule. A targetRef's
	// sectionName means different things by target kind: a listener on a
	// Gateway or ListenerSet, which route attachment has already applied, and
	// a route rule on a route, which only this carries.
	RuleSection *gatewayv1.SectionName
}

// BackendTrafficPolicyTargets returns the policy's targets, flattening the
// deprecated single TargetRef and the current TargetRefs into one list.
func BackendTrafficPolicyTargets(policy *egv1a1.BackendTrafficPolicy) []PolicyTarget {
	var targets []PolicyTarget
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		targets = append(targets, PolicyTarget{Kind: ref.Kind, Name: ref.Name, SectionName: ref.SectionName})
	}
	for _, ref := range policy.Spec.TargetRefs {
		targets = append(targets, PolicyTarget{Kind: ref.Kind, Name: ref.Name, SectionName: ref.SectionName})
	}
	return targets
}

// BackendTrafficPolicyCoverage returns every route the policy applies to,
// through its targetRef, its targetRefs, and its targetSelectors.
func BackendTrafficPolicyCoverage(lintCtx lintcontext.LintContext, policy *egv1a1.BackendTrafficPolicy) []CoveredRoute {
	var covered []CoveredRoute
	for _, target := range BackendTrafficPolicyTargets(policy) {
		inherited := IsParentKind(target.Kind)
		// On a parent target the sectionName names a listener, not a rule.
		var ruleSection *gatewayv1.SectionName
		if !inherited {
			ruleSection = target.SectionName
		}
		for _, route := range routesTargetedBy(lintCtx, policy.Namespace, target) {
			covered = append(covered, CoveredRoute{
				Target:      target,
				Route:       route,
				Inherited:   inherited,
				RuleSection: ruleSection,
			})
		}
	}
	for _, sel := range policy.Spec.TargetSelectors {
		for _, route := range routesSelectedBy(lintCtx, policy.Namespace, sel) {
			covered = append(covered, CoveredRoute{
				Target: PolicyTarget{Kind: route.Kind, Name: route.Name, ViaSelector: true},
				Route:  route,
			})
		}
	}
	return covered
}

// MoreSpecificPolicyCovers reports whether a BackendTrafficPolicy other than
// self names the given route rule directly. Envoy Gateway's own field
// documentation is explicit that with no mergeType set, "only the most
// specific configuration takes effect", so a route-level policy displaces an
// inherited Gateway-level or ListenerSet-level one entirely, and judging the
// inherited policy for that route would describe config that never applies.
func MoreSpecificPolicyCovers(lintCtx lintcontext.LintContext, route Route, rule *gatewayv1.SectionName, self *egv1a1.BackendTrafficPolicy) bool {
	for _, obj := range lintCtx.Objects() {
		other, ok := obj.K8sObject.(*egv1a1.BackendTrafficPolicy)
		if !ok || other == self {
			continue
		}
		for _, covered := range BackendTrafficPolicyCoverage(lintCtx, other) {
			if covered.Inherited || policyRouteKeyOf(covered.Route) != policyRouteKeyOf(route) {
				continue
			}
			if SectionCoversRule(covered.RuleSection, rule) {
				return true
			}
		}
	}
	return false
}

// SectionCoversRule reports whether a policy targetRef's sectionName covers a
// given route rule. No sectionName covers the whole route; a sectionName names
// one rule, and a rule with no name of its own cannot be the one named.
func SectionCoversRule(section, rule *gatewayv1.SectionName) bool {
	if section == nil {
		return true
	}
	return rule != nil && *rule == *section
}

// IsParentKind reports whether a policy target is an attachment parent, which
// is what makes the policy's coverage of a route inherited rather than direct.
func IsParentKind(kind gatewayv1.Kind) bool {
	return string(kind) == gwobjectkinds.Gateway || string(kind) == gwobjectkinds.ListenerSet
}

// IsRouteKind reports whether kind is one of the Gateway API route kinds.
func IsRouteKind(kind gatewayv1.Kind) bool {
	for _, k := range gwobjectkinds.RouteKinds {
		if string(kind) == k {
			return true
		}
	}
	return false
}

// routesTargetedBy returns the routes a policy target actually covers. A
// route-kind target resolves to that route directly. A Gateway target resolves
// to every route attached to that Gateway, whether attached to the Gateway
// itself or to a ListenerSet belonging to it, since Gateway API's attachment
// hierarchy runs Gateway to ListenerSet to Route and a Gateway-level policy
// applies to everything beneath it by default. A sectionName on a Gateway
// target narrows that to one listener.
func routesTargetedBy(lintCtx lintcontext.LintContext, policyNamespace string, target PolicyTarget) []Route {
	switch string(target.Kind) {
	case gwobjectkinds.Gateway:
		return routesAttachedToGateway(lintCtx, policyNamespace, string(target.Name), target.SectionName)
	case gwobjectkinds.ListenerSet:
		return routesAttachedToParent(lintCtx, gwobjectkinds.ListenerSet, policyNamespace, string(target.Name), target.SectionName)
	default:
		if !IsRouteKind(target.Kind) {
			return nil
		}
		for _, route := range Routes(lintCtx) {
			if route.Kind == target.Kind && route.Namespace == policyNamespace && route.Name == target.Name {
				return []Route{route}
			}
		}
		return nil
	}
}

// routesSelectedBy returns the routes a policy's targetSelector matches. Envoy
// Gateway selects by kind plus labels, scoped to a set of namespaces.
func routesSelectedBy(lintCtx lintcontext.LintContext, policyNamespace string, sel egv1a1.TargetSelector) []Route {
	if sel.Group != nil && string(*sel.Group) != gatewayv1.GroupName {
		return nil
	}
	if !IsRouteKind(sel.Kind) {
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

	var routes []Route
	for _, route := range Routes(lintCtx) {
		if route.Kind != sel.Kind {
			continue
		}
		if !targetNamespaceSelected(sel.Namespaces, policyNamespace, route.Namespace) {
			continue
		}
		if !selector.Matches(labels.Set(route.Labels)) {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

// targetNamespaceSelected reports whether a route's namespace is in scope for
// a target selector. "Same" (the default) means the policy's own namespace and
// "All" means any; a namespace label Selector is not resolved, since deciding
// it needs the Namespace objects, which a rendered manifest set rarely carries.
func targetNamespaceSelected(ns *egv1a1.TargetSelectorNamespaces, policyNamespace, routeNamespace string) bool {
	if ns == nil || ns.From == egv1a1.TargetNamespaceFromSame {
		return routeNamespace == policyNamespace
	}
	return ns.From == egv1a1.TargetNamespaceFromAll
}

// routesAttachedToGateway returns every route attached to the given Gateway,
// directly or through a ListenerSet whose parentRef names that Gateway. A
// non-nil listener narrows it to routes attached to that listener.
func routesAttachedToGateway(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string, listener *gatewayv1.SectionName) []Route {
	routes := routesAttachedToParent(lintCtx, gwobjectkinds.Gateway, gatewayNamespace, gatewayName, listener)

	seen := make(map[policyRouteKey]struct{}, len(routes))
	for _, r := range routes {
		seen[policyRouteKeyOf(r)] = struct{}{}
	}
	for _, ls := range listenerSetsBelongingTo(lintCtx, gatewayNamespace, gatewayName) {
		// A listener named on the Gateway target belongs to the Gateway, not to
		// the ListenerSet, so it does not narrow the ListenerSet's routes.
		for _, r := range routesAttachedToParent(lintCtx, gwobjectkinds.ListenerSet, ls.Namespace, ls.Name, nil) {
			if _, dup := seen[policyRouteKeyOf(r)]; dup {
				continue
			}
			seen[policyRouteKeyOf(r)] = struct{}{}
			routes = append(routes, r)
		}
	}
	return routes
}

// routesAttachedToParent returns every route whose parentRefs name the given
// Gateway or ListenerSet, optionally narrowed to one listener section.
func routesAttachedToParent(lintCtx lintcontext.LintContext, parentKind, parentNamespace, parentName string, listener *gatewayv1.SectionName) []Route {
	var routes []Route
	for _, route := range Routes(lintCtx) {
		for _, parent := range ParentsOf(route) {
			if parent.Matches(parentKind, parentNamespace, parentName) && parent.CoversListener(listener) {
				routes = append(routes, route)
				break
			}
		}
	}
	return routes
}

func listenerSetsBelongingTo(lintCtx lintcontext.LintContext, gatewayNamespace, gatewayName string) []ObjectRef {
	var sets []ObjectRef
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
		if got := ListenerSetParent(ls); got.Namespace == gatewayNamespace && got.Name == gatewayName {
			sets = append(sets, ObjectRef{Kind: gwobjectkinds.ListenerSet, Namespace: ls.Namespace, Name: ls.Name})
		}
	}
	return sets
}

// policyRouteKey identifies a route for de-duplication: a route can name both
// a Gateway and one of its ListenerSets in parentRefs.
type policyRouteKey struct {
	kind      gatewayv1.Kind
	namespace string
	name      gatewayv1.ObjectName
}

func policyRouteKeyOf(r Route) policyRouteKey {
	return policyRouteKey{kind: r.Kind, namespace: r.Namespace, name: r.Name}
}
