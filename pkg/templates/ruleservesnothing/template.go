// Package ruleservesnothing flags a route rule with no backendRefs and no
// filter that answers a request by itself. Both fields are optional, so the
// rule is schema-valid and applies cleanly, and it still matches traffic: the
// rule claims the requests its matches describe, takes them from whatever
// broader rule would otherwise have served them, and then has nowhere to send
// them. Gateway API's own wording for a rule with nothing to route to and no
// filters is a 500. Deleting the backendRefs of a retired rule while leaving
// its matches behind is the usual way to get here.
//
// In practice this is an HTTPRoute or GRPCRoute finding, since those are the
// kinds whose rules may carry filters instead of backends. TCP, TLS and UDP
// rules have no filters and their CRDs require at least one backendRef, so
// they reach the finding only in a manifest a schema validator would already
// reject. The check still covers them rather than special-casing kinds, since
// gwlint is often pointed at YAML no CRD has validated.
//
// The check reads one route and nothing else, so a partial manifest set cannot
// make it fire: there is no reference here to resolve against objects that may
// live in another repo. An ExtensionRef filter does name another object, but
// the check only asks whether such a filter is present, never whether what it
// names exists.
package ruleservesnothing

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

const templateKey = "rule-serves-nothing"

// terminatingFilters are the rule-level filters that produce a response on
// their own, so a rule carrying one still serves something without a backend.
// RequestRedirect answers with a 3xx and never reaches a backend. ExtensionRef
// hands the request to an implementation-specific extension whose behaviour
// gwlint cannot see, so it counts as serving something rather than being
// guessed at. Every other filter type, header modifiers and mirroring and
// rewriting alike, only decorates a request that still has to reach a backend.
// GRPCRoute has no RequestRedirect, and its ExtensionRef is a separate constant
// spelling the same string, so matching on the string covers both kinds.
var terminatingFilters = map[string]struct{}{
	string(gatewayv1.HTTPRouteFilterRequestRedirect): {},
	string(gatewayv1.HTTPRouteFilterExtensionRef):    {},
}

func init() {
	templates.Register(check.Template{
		HumanName: "Route rule serves nothing",
		Key:       templateKey,
		Description: "Flag route rules with no backendRefs and no filter that produces a response, " +
			"so requests matching the rule are answered with an error",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: gwobjectkinds.RouteKinds,
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, rule := range gatewayapi.RulesOf(object.K8sObject) {
		if len(rule.BackendRefs) > 0 || servesAResponse(rule) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q %s has no backendRefs and no filter that produces a response, "+
					"so requests matching the rule are answered with an error instead of being served",
				route.Kind, route.Name, rule.Describe()),
		})
	}
	return diagnostics
}

func servesAResponse(rule gatewayapi.Rule) bool {
	for _, filterType := range rule.Filters {
		if _, ok := terminatingFilters[filterType]; ok {
			return true
		}
	}
	return false
}
