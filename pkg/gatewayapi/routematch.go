package gatewayapi

import (
	"fmt"
	"sort"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// HTTPMatch is one HTTPRouteMatch flattened to a comparable form. Gateway API
// breaks a tie between two routes by comparing the specificity of their
// matches, so a check that reasons about precedence needs a value it can
// compare for equality rather than the nested pointer-laden struct.
type HTTPMatch struct {
	// Key is equal for exactly those matches that select the same requests.
	Key string
	// Describe renders the match for a diagnostic message.
	Describe string
}

// HTTPMatchesOf returns every request match an HTTPRoute declares, across all
// of its rules, and reports whether obj was an HTTPRoute at all. Defaults are
// applied here because a decoded manifest carries only what the author wrote,
// while the apiserver would have filled the rest in: a rule with no matches
// matches every request under path prefix "/", an omitted path type is
// PathPrefix, and an omitted header or query param type is Exact.
func HTTPMatchesOf(obj k8sutil.Object) ([]HTTPMatch, bool) {
	var rules []gatewayv1.HTTPRouteRule
	switch route := obj.(type) {
	case *gatewayv1.HTTPRoute:
		rules = route.Spec.Rules
	case *gatewayv1b1.HTTPRoute:
		rules = (*gatewayv1.HTTPRoute)(route).Spec.Rules
	default:
		return nil, false
	}

	var matches []HTTPMatch
	for _, rule := range rules {
		if len(rule.Matches) == 0 {
			matches = append(matches, canonicalHTTPMatch(gatewayv1.HTTPRouteMatch{}))
			continue
		}
		for _, m := range rule.Matches {
			matches = append(matches, canonicalHTTPMatch(m))
		}
	}
	return matches, true
}

func canonicalHTTPMatch(m gatewayv1.HTTPRouteMatch) HTTPMatch {
	pathType, pathValue := gatewayv1.PathMatchPathPrefix, "/"
	if m.Path != nil {
		if m.Path.Type != nil {
			pathType = *m.Path.Type
		}
		if m.Path.Value != nil {
			pathValue = *m.Path.Value
		}
	}

	parts := []string{fmt.Sprintf("path=%s:%s", pathType, pathValue)}
	describe := fmt.Sprintf("%s %q", pathType, pathValue)

	if m.Method != nil {
		parts = append(parts, "method="+string(*m.Method))
		describe += fmt.Sprintf(" and method %s", *m.Method)
	}

	// HTTP header names are case insensitive, so two routes spelling the same
	// header differently still select the same requests. Query parameter names
	// are case sensitive and are left alone.
	headers := make([]string, 0, len(m.Headers))
	for _, h := range m.Headers {
		matchType := gatewayv1.HeaderMatchExact
		if h.Type != nil {
			matchType = *h.Type
		}
		headers = append(headers, fmt.Sprintf("%s:%s:%s", matchType, strings.ToLower(string(h.Name)), h.Value))
	}
	params := make([]string, 0, len(m.QueryParams))
	for _, q := range m.QueryParams {
		matchType := gatewayv1.QueryParamMatchExact
		if q.Type != nil {
			matchType = *q.Type
		}
		params = append(params, fmt.Sprintf("%s:%s:%s", matchType, q.Name, q.Value))
	}
	// Match order carries no meaning, so sort before joining or two identical
	// matches written in a different order would look distinct.
	sort.Strings(headers)
	sort.Strings(params)
	if len(headers) > 0 {
		parts = append(parts, "headers="+strings.Join(headers, ","))
		describe += fmt.Sprintf(" and %d header %s", len(headers), matchNoun(len(headers)))
	}
	if len(params) > 0 {
		parts = append(parts, "query="+strings.Join(params, ","))
		describe += fmt.Sprintf(" and %d query parameter %s", len(params), matchNoun(len(params)))
	}

	return HTTPMatch{Key: strings.Join(parts, ";"), Describe: describe}
}

func matchNoun(n int) string {
	if n == 1 {
		return "match"
	}
	return "matches"
}
