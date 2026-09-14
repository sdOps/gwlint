package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// RuleNamesOf returns the name of every rule on a route, in spec order, with a
// nil entry for a rule that sets none. A rule name is identity rather than
// documentation: it is what a policy targetRef's sectionName addresses. A
// check reasoning about it needs every rule, including rules that forward
// nowhere, which is why this does not read Route.BackendRefs.
//
// Returns nil for anything that is not a route kind. The v1alpha2 rule types
// are distinct named types rather than aliases, so their slices cannot be
// converted and each needs its own case.
func RuleNamesOf(obj k8sutil.Object) []*gatewayv1.SectionName {
	switch route := obj.(type) {
	case *gatewayv1.HTTPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.HTTPRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1b1.HTTPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.HTTPRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1.GRPCRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.GRPCRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1a2.GRPCRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.GRPCRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1.TCPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.TCPRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1a2.TCPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1a2.TCPRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1.TLSRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.TLSRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1a3.TLSRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.TLSRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1a2.TLSRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1a2.TLSRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1.UDPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1.UDPRouteRule) *gatewayv1.SectionName { return r.Name })
	case *gatewayv1a2.UDPRoute:
		return ruleNames(route.Spec.Rules, func(r gatewayv1a2.UDPRouteRule) *gatewayv1.SectionName { return r.Name })
	default:
		return nil
	}
}

func ruleNames[T any](rules []T, nameOf func(T) *gatewayv1.SectionName) []*gatewayv1.SectionName {
	names := make([]*gatewayv1.SectionName, 0, len(rules))
	for _, rule := range rules {
		names = append(names, nameOf(rule))
	}
	return names
}
