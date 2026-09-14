// Package conflictingpolicies flags two BackendTrafficPolicy objects that
// target the same thing. Envoy Gateway attaches at most one
// BackendTrafficPolicy per target: the translator marks the target attached
// when the first policy claims it and rejects every later one with
// Accepted=False, reason Conflicted ("Unable to target ... another
// BackendTrafficPolicy has already attached to it"). The losing policy's
// timeouts, retries and circuit breakers never reach the data plane, and
// nothing in the cluster looks broken. Which policy wins is decided by
// creation timestamp, falling back to namespace and name, so it is not even
// stable across a re-apply.
//
// Both manifests are individually valid, so a schema validator has nothing to
// say about them. The conflict only exists between two files.
//
// Targets are compared the way the translator keys them: group, kind, the
// policy's own namespace (a BackendTrafficPolicy targetRef is local and cannot
// name another namespace) and sectionName. A sectionName makes a policy claim a
// narrower slot, a listener on a Gateway or ListenerSet and a rule on a route,
// so a policy scoped to one rule does not conflict with one covering the whole
// route.
//
// Only one of the conflicting policies is reported, the first by name, so a
// pair produces one finding rather than two accusations of the same thing.
//
// No partial-set guard applies here, and none should: every object the finding
// depends on is a policy that is present. The check deliberately does not
// require the target to be in the set, since policies and the routes they
// target routinely live in different repos, and two policies fighting over one
// target is a bug wherever that target is defined.
//
// Policies matched by spec.targetSelectors are out of scope. Deciding those
// needs the labels of the targets, which a policies-only manifest set does not
// carry, and guessing would mean false positives.
package conflictingpolicies

import (
	"fmt"
	"sort"
	"strings"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "conflicting-policies"

func init() {
	templates.Register(check.Template{
		HumanName: "Conflicting BackendTrafficPolicies",
		Key:       templateKey,
		Description: "Flag two BackendTrafficPolicies targeting the same object and section, where Envoy " +
			"Gateway attaches only one and silently rejects the rest as Conflicted",
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

// target is how Envoy Gateway's translator keys an attachment slot. Namespace
// is the policy's own, since a BackendTrafficPolicy targetRef is a
// LocalPolicyTargetReference and cannot reach out of its namespace.
type target struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
	Section   string
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	policy, ok := object.K8sObject.(*egv1a1.BackendTrafficPolicy)
	if !ok {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, t := range targetsOf(policy) {
		rivals := otherPoliciesTargeting(lintCtx, policy, t)
		if len(rivals) == 0 {
			continue
		}
		// Every policy in the group sees the same conflict, so let the first by
		// name report it. Names are unique within a namespace, which makes the
		// choice deterministic without depending on iteration order.
		if !isFirstByName(policy.Name, rivals) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"BackendTrafficPolicy and %s both target %s; Envoy Gateway attaches only one "+
					"BackendTrafficPolicy per target and rejects the rest as Conflicted, so all but one "+
					"are silently ignored, and which one wins depends on creation timestamps that the "+
					"manifests do not carry",
				describePolicies(rivals), describeTarget(t)),
		})
	}
	return diagnostics
}

// targetsOf returns the distinct slots a policy claims. The deprecated
// singular targetRef and the current targetRefs claim slots the same way, and
// a policy naming the same slot twice is claiming it once.
func targetsOf(policy *egv1a1.BackendTrafficPolicy) []target {
	var refs []gatewayv1.LocalPolicyTargetReferenceWithSectionName
	//nolint:staticcheck // deprecated field still seen in the wild; must still be checked
	if ref := policy.Spec.TargetRef; ref != nil {
		refs = append(refs, *ref)
	}
	refs = append(refs, policy.Spec.TargetRefs...)

	seen := make(map[target]struct{}, len(refs))
	targets := make([]target, 0, len(refs))
	for _, ref := range refs {
		t := target{
			Group:     string(ref.Group),
			Kind:      string(ref.Kind),
			Namespace: policy.Namespace,
			Name:      string(ref.Name),
		}
		if ref.SectionName != nil {
			t.Section = string(*ref.SectionName)
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		targets = append(targets, t)
	}
	return targets
}

func otherPoliciesTargeting(lintCtx lintcontext.LintContext, self *egv1a1.BackendTrafficPolicy, t target) []string {
	var names []string
	for _, obj := range lintCtx.Objects() {
		other, ok := obj.K8sObject.(*egv1a1.BackendTrafficPolicy)
		if !ok || other == self {
			continue
		}
		for _, otherTarget := range targetsOf(other) {
			if otherTarget == t {
				names = append(names, other.Name)
				break
			}
		}
	}
	sort.Strings(names)
	return names
}

func isFirstByName(name string, rivals []string) bool {
	for _, rival := range rivals {
		if rival < name {
			return false
		}
	}
	return true
}

func describePolicies(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, fmt.Sprintf("BackendTrafficPolicy %q", n))
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return fmt.Sprintf("%s and %s", strings.Join(quoted[:len(quoted)-1], ", "), quoted[len(quoted)-1])
}

// describeTarget names the slot in the terms the reader wrote it in. The same
// sectionName field means a listener on a Gateway or ListenerSet and a rule on
// a route, so saying which one avoids sending the reader to the wrong object.
func describeTarget(t target) string {
	if t.Section == "" {
		return fmt.Sprintf("%s %q", t.Kind, t.Name)
	}
	slot := "rule"
	if t.Kind == gwobjectkinds.Gateway || t.Kind == gwobjectkinds.ListenerSet {
		slot = "listener"
	}
	return fmt.Sprintf("%s %q of %s %q", slot, t.Section, t.Kind, t.Name)
}
