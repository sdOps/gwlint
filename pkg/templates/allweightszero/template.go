// Package allweightszero flags a route rule that weights every one of its
// backendRefs 0. Weight is the share of traffic a backend gets, so a rule whose
// weights sum to nothing still matches requests, wins them from any less
// specific rule, and then forwards them nowhere. Each weight is individually
// valid, which is why the CRD's schema accepts it: 0 is in range, and no
// validation looks across the list. It is what a half-finished traffic shift
// leaves behind, a canary dialled to 0 with the old backend already removed.
//
// The check reads one route and nothing else, so a partial manifest set cannot
// make it fire: there is no reference here to resolve against objects that may
// live in another repo.
package allweightszero

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "all-weights-zero"

func init() {
	templates.Register(check.Template{
		HumanName: "All backendRef weights zero",
		Key:       templateKey,
		Description: "Flag route rules that set weight 0 on every backendRef, " +
			"so the rule matches requests and then forwards them to nothing",
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
		// A rule with no backendRefs at all serves nothing for a different
		// reason, and rule-serves-nothing is the check that says so.
		if len(rule.BackendRefs) == 0 || !allWeightsZero(rule) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q %s sets weight 0 on every one of its %d backendRefs, "+
					"so requests matching the rule are forwarded to none of them",
				route.Kind, route.Name, rule.Describe(), len(rule.BackendRefs)),
		})
	}
	return diagnostics
}

// allWeightsZero reports whether every backendRef in the rule is explicitly
// weighted 0. An omitted weight defaults to 1, so a nil weight is a backend
// that does take traffic and clears the rule.
func allWeightsZero(rule gatewayapi.Rule) bool {
	for _, ref := range rule.BackendRefs {
		if ref.Weight == nil || *ref.Weight != 0 {
			return false
		}
	}
	return true
}
