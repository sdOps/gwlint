// Package danglingpolicytarget flags a BackendTrafficPolicy whose targetRef or
// targetRefs name a Gateway, ListenerSet or route that is not present. Policy
// attachment is by name: nothing rejects a name that resolves to nothing, the
// policy is simply never attached, and every timeout, retry and circuit
// breaker it carries quietly does not apply. A schema validator sees a
// well-formed reference and passes it, because whether the object exists is
// not a property of the document.
//
// Only the named targets are judged. A targetSelector that matches no route is
// a different condition: it selects by label rather than naming anything, so
// its absence of matches is not a dangling reference.
//
// The guard: judge a target kind only in a namespace the manifest set actually
// describes, meaning one where the set already carries at least one object of
// that kind. Policies routinely ship in the same chart as the routes they
// target while the Gateway comes from a platform repo, and linting either half
// on its own is normal. A namespace the set says nothing about is not evidence
// that the target is missing.
package danglingpolicytarget

import (
	"fmt"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "dangling-policy-target"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling Policy targetRef",
		Key:       templateKey,
		Description: "Flag BackendTrafficPolicies whose targetRefs name a Gateway, ListenerSet or route " +
			"that is not present, so the policy attaches to nothing and its settings never apply",
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

// namespacedKind is a kind scoped to a namespace, which is the granularity at
// which the manifest set either describes something or does not.
type namespacedKind struct {
	kind      string
	namespace string
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	policy, ok := object.K8sObject.(*egv1a1.BackendTrafficPolicy)
	if !ok {
		return nil
	}

	known := policyTargets(lintCtx)
	described := make(map[namespacedKind]bool, len(known))
	for ref := range known {
		described[namespacedKind{kind: ref.Kind, namespace: ref.Namespace}] = true
	}

	var diagnostics []diagnostic.Diagnostic
	for _, ref := range targetRefsOf(policy) {
		// Envoy Gateway's CRD permits no group but Gateway API on these refs, so
		// an omitted one still means a Gateway API target. Anything else belongs
		// to another implementation and is not ours to resolve.
		if group := string(ref.Group); group != "" && group != gatewayapi.GroupName {
			continue
		}
		kind := string(ref.Kind)
		if !targetableKind(kind) {
			continue
		}
		// A BackendTrafficPolicy targetRef is a LocalPolicyTargetReference: it
		// carries no namespace and only ever resolves in the policy's own.
		if !described[namespacedKind{kind: kind, namespace: policy.Namespace}] {
			continue
		}
		name := string(ref.Name)
		if _, found := known[gatewayapi.ObjectRef{Kind: kind, Namespace: policy.Namespace, Name: name}]; found {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"BackendTrafficPolicy %q targets %s %q in namespace %q, which is not present; "+
					"the policy attaches to nothing and its settings never apply",
				policy.Name, kind, name, policy.Namespace),
		})
	}
	return diagnostics
}

// targetRefsOf returns every named target on the policy. The singular
// targetRef is deprecated in favour of targetRefs but is still accepted and
// still widely written, so both have to be walked.
func targetRefsOf(policy *egv1a1.BackendTrafficPolicy) []gatewayv1.LocalPolicyTargetReferenceWithSectionName {
	refs := make([]gatewayv1.LocalPolicyTargetReferenceWithSectionName, 0, len(policy.Spec.TargetRefs)+1)
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		refs = append(refs, *ref)
	}
	return append(refs, policy.Spec.TargetRefs...)
}

// targetableKind reports whether gwlint can resolve a target of this kind.
// Envoy Gateway's own kubebuilder validation on BackendTrafficPolicySpec lists
// the permitted kinds as Gateway, ListenerSet and the five route kinds.
func targetableKind(kind string) bool {
	if kind == gwobjectkinds.Gateway || kind == gwobjectkinds.ListenerSet {
		return true
	}
	for _, routeKind := range gwobjectkinds.RouteKinds {
		if kind == routeKind {
			return true
		}
	}
	return false
}

// policyTargets indexes every object a policy targetRef can name, across every
// served API version, which is how the check tells a typo from a target that
// lives in a manifest set somebody else renders.
func policyTargets(lintCtx lintcontext.LintContext) map[gatewayapi.ObjectRef]struct{} {
	targets := gatewayapi.GatewaysAndListenerSets(lintCtx)
	for _, route := range gatewayapi.Routes(lintCtx) {
		targets[gatewayapi.ObjectRef{
			Kind: string(route.Kind), Namespace: route.Namespace, Name: string(route.Name),
		}] = struct{}{}
	}
	return targets
}
