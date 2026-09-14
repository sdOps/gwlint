// Package duplicaterulename flags a route with two rules sharing a name.
// Gateway API says a rule name must be unique within a route because a policy
// targetRef's sectionName addresses a rule by it; with the name repeated, the
// sectionName picks out no single rule, and which one the implementation
// attaches the policy to is not something the manifests decide. The same name
// is also how a route's status reports per-rule conditions back.
//
// A schema validator misses this. The uniqueness rule is marked
// `gateway:experimental:validation` upstream, so it reaches only the
// experimental-channel CRD; against the standard channel even the API server
// accepts the duplicate. Offline validators such as kubeconform do not
// evaluate CEL validations at all, so neither channel catches it there.
//
// The check reads one route in isolation and resolves no references, so the
// partial manifest set guard other checks need does not apply here: whether
// the Gateway or the policy is in the same lint run changes nothing about
// whether the route names its own rules twice.
package duplicaterulename

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

const templateKey = "duplicate-rule-name"

func init() {
	templates.Register(check.Template{
		HumanName: "Duplicate Route rule name",
		Key:       templateKey,
		Description: "Flag routes with two rules sharing a name, which a policy targetRef sectionName " +
			"cannot then address unambiguously",
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

	names := gatewayapi.RuleNamesOf(object.K8sObject)
	counts := make(map[gatewayv1.SectionName]int, len(names))
	for _, name := range names {
		// An unnamed rule is the norm and cannot be addressed by sectionName at
		// all, so it collides with nothing. An empty name is schema-invalid,
		// which is kubeconform's finding rather than this one.
		if name == nil || *name == "" {
			continue
		}
		counts[*name]++
	}

	// Walk the rules again rather than the map so findings come out in spec
	// order, and report each repeated name once at the rule that repeats it.
	var diagnostics []diagnostic.Diagnostic
	reported := make(map[gatewayv1.SectionName]bool, len(counts))
	for _, name := range names {
		if name == nil || counts[*name] < 2 || reported[*name] {
			continue
		}
		reported[*name] = true
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q has %d rules named %q; a policy targetRef sectionName naming that rule "+
					"cannot address one of them unambiguously",
				route.Kind, route.Name, counts[*name], *name),
		})
	}
	return diagnostics
}
