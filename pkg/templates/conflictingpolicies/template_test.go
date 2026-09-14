package conflictingpolicies

import (
	"fmt"
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const namespace = "default"

func TestConflictingPolicies(t *testing.T) {
	suite.Run(t, new(ConflictingPoliciesTestSuite))
}

type ConflictingPoliciesTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *ConflictingPoliciesTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func sectionPtr(n gatewayv1.SectionName) *gatewayv1.SectionName { return &n }

func ref(kind, name string, section *gatewayv1.SectionName) gatewayv1.LocalPolicyTargetReferenceWithSectionName {
	return gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
			Group: gatewayv1.GroupName,
			Kind:  gatewayv1.Kind(kind),
			Name:  gatewayv1.ObjectName(name),
		},
		SectionName: section,
	}
}

func (s *ConflictingPoliciesTestSuite) addPolicy(name, ns string, refs ...gatewayv1.LocalPolicyTargetReferenceWithSectionName) {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{TargetRefs: refs},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject(ns+"/"+name, policy)
}

// addLegacyPolicy uses the deprecated singular targetRef, which claims the same
// attachment slot as targetRefs does.
func (s *ConflictingPoliciesTestSuite) addLegacyPolicy(name, ns string, r gatewayv1.LocalPolicyTargetReferenceWithSectionName) {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{TargetRef: &r},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject(ns+"/"+name, policy)
}

func want(rivals, targetDesc string) string {
	return fmt.Sprintf(
		"BackendTrafficPolicy and %s both target %s; Envoy Gateway attaches only one "+
			"BackendTrafficPolicy per target and rejects the rest as Conflicted, so all but one "+
			"are silently ignored, and which one wins depends on creation timestamps that the "+
			"manifests do not carry",
		rivals, targetDesc)
}

func (s *ConflictingPoliciesTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *ConflictingPoliciesTestSuite) TestFlagsTwoPoliciesTargetingTheSameRoute() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", nil))

	// Only the first by name reports, so a pair is one finding, not two.
	s.expect(map[string][]string{
		"alpha-policy": {want(`BackendTrafficPolicy "beta-policy"`, `HTTPRoute "checkout-route"`)},
	})
}

func (s *ConflictingPoliciesTestSuite) TestFlagsTwoPoliciesTargetingTheSameGateway() {
	s.addPolicy("alpha-policy", namespace, ref("Gateway", "edge-gateway", nil))
	s.addPolicy("beta-policy", namespace, ref("Gateway", "edge-gateway", nil))

	s.expect(map[string][]string{
		"alpha-policy": {want(`BackendTrafficPolicy "beta-policy"`, `Gateway "edge-gateway"`)},
	})
}

func (s *ConflictingPoliciesTestSuite) TestPassesWhenTheTargetsDiffer() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "orders-route", nil))

	s.expect(nil)
}

// The kind is part of the attachment slot, so a Gateway and a route of the
// same name are two different targets.
func (s *ConflictingPoliciesTestSuite) TestPassesWhenTheKindsDiffer() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "shared-name", nil))
	s.addPolicy("beta-policy", namespace, ref("Gateway", "shared-name", nil))

	s.expect(nil)
}

// A BackendTrafficPolicy targetRef is local, so two policies in different
// namespaces never reach the same object.
func (s *ConflictingPoliciesTestSuite) TestPassesAcrossNamespaces() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", "payments", ref("HTTPRoute", "checkout-route", nil))

	s.expect(nil)
}

// Envoy Gateway tracks whole-route attachment separately from per-rule
// attachment, so two distinct sectionNames claim two distinct slots.
func (s *ConflictingPoliciesTestSuite) TestPassesWhenSectionNamesDiffer() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", sectionPtr("rule-a")))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", sectionPtr("rule-b")))

	s.expect(nil)
}

// A policy covering the whole route and one scoped to a single rule claim
// different slots too, so neither displaces the other.
func (s *ConflictingPoliciesTestSuite) TestPassesWhenOnlyOnePolicyIsRuleScoped() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", sectionPtr("rule-a")))

	s.expect(nil)
}

func (s *ConflictingPoliciesTestSuite) TestFlagsTheSameRouteRuleClaimedTwice() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", sectionPtr("rule-a")))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", sectionPtr("rule-a")))

	s.expect(map[string][]string{
		"alpha-policy": {
			want(`BackendTrafficPolicy "beta-policy"`, `rule "rule-a" of HTTPRoute "checkout-route"`),
		},
	})
}

// On a Gateway the same sectionName names a listener, which the message has to
// say so the reader looks in the right place.
func (s *ConflictingPoliciesTestSuite) TestNamesAListenerOnAGatewayTarget() {
	s.addPolicy("alpha-policy", namespace, ref("Gateway", "edge-gateway", sectionPtr("https")))
	s.addPolicy("beta-policy", namespace, ref("Gateway", "edge-gateway", sectionPtr("https")))

	s.expect(map[string][]string{
		"alpha-policy": {
			want(`BackendTrafficPolicy "beta-policy"`, `listener "https" of Gateway "edge-gateway"`),
		},
	})
}

func (s *ConflictingPoliciesTestSuite) TestReportsEveryRivalOnce() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("gamma-policy", namespace, ref("HTTPRoute", "checkout-route", nil))

	s.expect(map[string][]string{
		"alpha-policy": {
			want(`BackendTrafficPolicy "beta-policy" and BackendTrafficPolicy "gamma-policy"`,
				`HTTPRoute "checkout-route"`),
		},
	})
}

// A policy with several targetRefs can be in several conflicts at once.
func (s *ConflictingPoliciesTestSuite) TestReportsEachConflictingTarget() {
	s.addPolicy("alpha-policy", namespace,
		ref("HTTPRoute", "checkout-route", nil),
		ref("HTTPRoute", "orders-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("gamma-policy", namespace, ref("HTTPRoute", "orders-route", nil))

	s.expect(map[string][]string{
		"alpha-policy": {
			want(`BackendTrafficPolicy "beta-policy"`, `HTTPRoute "checkout-route"`),
			want(`BackendTrafficPolicy "gamma-policy"`, `HTTPRoute "orders-route"`),
		},
	})
}

// The deprecated singular targetRef claims the same slot as targetRefs, so the
// two forms conflict with each other.
func (s *ConflictingPoliciesTestSuite) TestFlagsTheDeprecatedTargetRefAgainstTargetRefs() {
	s.addLegacyPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "checkout-route", nil))

	s.expect(map[string][]string{
		"alpha-policy": {want(`BackendTrafficPolicy "beta-policy"`, `HTTPRoute "checkout-route"`)},
	})
}

// One policy naming the same target twice claims the slot once, so it is not
// in conflict with itself.
func (s *ConflictingPoliciesTestSuite) TestAPolicyDoesNotConflictWithItself() {
	s.addPolicy("alpha-policy", namespace,
		ref("HTTPRoute", "checkout-route", nil),
		ref("HTTPRoute", "checkout-route", nil))

	s.expect(nil)
}

func (s *ConflictingPoliciesTestSuite) TestPassesWithASinglePolicy() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "checkout-route", nil))

	s.expect(nil)
}

// The conflict is decided entirely by the policies in front of us, so it is
// reported whether or not the target itself was passed in. Policies and the
// routes they target routinely live in separate repos.
func (s *ConflictingPoliciesTestSuite) TestDoesNotNeedTheTargetInTheSet() {
	s.addPolicy("alpha-policy", namespace, ref("HTTPRoute", "route-defined-elsewhere", nil))
	s.addPolicy("beta-policy", namespace, ref("HTTPRoute", "route-defined-elsewhere", nil))

	s.expect(map[string][]string{
		"alpha-policy": {
			want(`BackendTrafficPolicy "beta-policy"`, `HTTPRoute "route-defined-elsewhere"`),
		},
	})
}
