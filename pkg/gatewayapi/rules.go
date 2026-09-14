package gatewayapi

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1a3 "sigs.k8s.io/gateway-api/apis/v1alpha3"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Rule is one entry of a route's spec.rules, kept whole. Route flattens
// backendRefs across every rule, which is what a check resolving references
// wants; a check asking whether a rule serves anything needs the rule boundary
// back, because the answer is about one rule at a time.
type Rule struct {
	// Index is the rule's position in spec.rules. Rule names are optional, so
	// for most rules this is the only way to point at one.
	Index int
	Name  *gatewayv1.SectionName
	// BackendRefs are the rule's backendRefs, unwrapped from the per-kind
	// wrapper types, which differ only by the filters they add.
	BackendRefs []gatewayv1.BackendRef
	// Filters holds the rule-level filter types. TCP, TLS and UDP rules have no
	// filters at all, so theirs is always empty.
	Filters []string
}

// Describe names the rule the way a diagnostic should refer to it, falling
// back to the index because spec.rules[].name is optional.
func (r Rule) Describe() string {
	if r.Name != nil && *r.Name != "" {
		return fmt.Sprintf("rule %q", string(*r.Name))
	}
	return fmt.Sprintf("rule %d", r.Index)
}

// RulesOf returns the rules of obj in spec order if it is any Gateway API
// route kind, and nil otherwise. It covers the same types as AsRoute and has
// to keep covering them: a kind handled there but missed here would make a
// per-rule check silently pass. v1beta1 and v1alpha3 route types are Go
// aliases of their v1 equivalents, and the v1alpha2 stream rule types are
// defined over v1's, so every version is read through the v1 field names.
func RulesOf(obj k8sutil.Object) []Rule {
	switch route := obj.(type) {
	case *gatewayv1.HTTPRoute:
		return httpRules(route.Spec.Rules)
	case *gatewayv1b1.HTTPRoute:
		return httpRules((*gatewayv1.HTTPRoute)(route).Spec.Rules)
	case *gatewayv1.GRPCRoute:
		return grpcRules(route.Spec.Rules)
	case *gatewayv1a2.GRPCRoute:
		return grpcRules((*gatewayv1.GRPCRoute)(route).Spec.Rules)
	case *gatewayv1.TCPRoute:
		return tcpRules(route.Spec.Rules)
	case *gatewayv1a2.TCPRoute:
		return alpha2TCPRules(route.Spec.Rules)
	case *gatewayv1.TLSRoute:
		return tlsRules(route.Spec.Rules)
	case *gatewayv1a3.TLSRoute:
		return tlsRules((*gatewayv1.TLSRoute)(route).Spec.Rules)
	case *gatewayv1a2.TLSRoute:
		return alpha2TLSRules(route.Spec.Rules)
	case *gatewayv1.UDPRoute:
		return udpRules(route.Spec.Rules)
	case *gatewayv1a2.UDPRoute:
		return alpha2UDPRules(route.Spec.Rules)
	default:
		return nil
	}
}

func httpRules(rules []gatewayv1.HTTPRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		rule := Rule{Index: i, Name: r.Name}
		for _, br := range r.BackendRefs {
			rule.BackendRefs = append(rule.BackendRefs, br.BackendRef)
		}
		for _, f := range r.Filters {
			rule.Filters = append(rule.Filters, string(f.Type))
		}
		out = append(out, rule)
	}
	return out
}

func grpcRules(rules []gatewayv1.GRPCRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		rule := Rule{Index: i, Name: r.Name}
		for _, br := range r.BackendRefs {
			rule.BackendRefs = append(rule.BackendRefs, br.BackendRef)
		}
		for _, f := range r.Filters {
			rule.Filters = append(rule.Filters, string(f.Type))
		}
		out = append(out, rule)
	}
	return out
}

// The stream rule types carry the same two fields in six distinct Go types, so
// each needs its own loop even though the bodies are identical.
func tcpRules(rules []gatewayv1.TCPRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}

func alpha2TCPRules(rules []gatewayv1a2.TCPRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}

func tlsRules(rules []gatewayv1.TLSRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}

func alpha2TLSRules(rules []gatewayv1a2.TLSRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}

func udpRules(rules []gatewayv1.UDPRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}

func alpha2UDPRules(rules []gatewayv1a2.UDPRouteRule) []Rule {
	out := make([]Rule, 0, len(rules))
	for i, r := range rules {
		out = append(out, Rule{Index: i, Name: r.Name, BackendRefs: r.BackendRefs})
	}
	return out
}
