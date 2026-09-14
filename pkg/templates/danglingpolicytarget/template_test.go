package danglingpolicytarget

import (
	"fmt"
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	policyName = "orders-timeout-policy"
	namespace  = "default"
)

func TestDanglingPolicyTarget(t *testing.T) {
	suite.Run(t, new(DanglingPolicyTargetTestSuite))
}

type DanglingPolicyTargetTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DanglingPolicyTargetTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func sectionPtr(s gatewayv1.SectionName) *gatewayv1.SectionName { return &s }

func (s *DanglingPolicyTargetTestSuite) addGateway() {
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "real-gateway", Namespace: namespace}}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-"+namespace, gw)
}

func (s *DanglingPolicyTargetTestSuite) addListenerSet(name, ns string) {
	ls := &gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	ls.SetGroupVersionKind(gvk("ListenerSet"))
	s.ctx.AddObject("listenerset-"+ns+"-"+name, ls)
}

func (s *DanglingPolicyTargetTestSuite) addHTTPRoute(name, ns string) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject("httproute-"+ns+"-"+name, route)
}

func (s *DanglingPolicyTargetTestSuite) addTCPRoute(name, ns string) {
	route := &gatewayv1.TCPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	route.SetGroupVersionKind(gvk("TCPRoute"))
	s.ctx.AddObject("tcproute-"+ns+"-"+name, route)
}

// addBetaHTTPRoute adds an HTTPRoute written as v1beta1. Which served version
// a manifest uses is the author's choice, not a different object.
func (s *DanglingPolicyTargetTestSuite) addBetaHTTPRoute(name, ns string) {
	route := &gatewayv1b1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	route.SetGroupVersionKind(
		schema.GroupVersion{Group: gatewayv1.GroupName, Version: "v1beta1"}.WithKind("HTTPRoute"))
	s.ctx.AddObject("httproute-beta-"+ns+"-"+name, route)
}

func targetRef(kind, name string) gatewayv1.LocalPolicyTargetReferenceWithSectionName {
	return gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
			Group: gatewayv1.GroupName,
			Kind:  gatewayv1.Kind(kind),
			Name:  gatewayv1.ObjectName(name),
		},
	}
}

func (s *DanglingPolicyTargetTestSuite) newPolicy() *egv1a1.BackendTrafficPolicy {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: namespace},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	return policy
}

func (s *DanglingPolicyTargetTestSuite) addPolicy(refs ...gatewayv1.LocalPolicyTargetReferenceWithSectionName) {
	policy := s.newPolicy()
	policy.Spec.TargetRefs = refs
	s.ctx.AddObject(policyName, policy)
}

// addPolicyWithDeprecatedTargetRef uses BackendTrafficPolicy's singular
// targetRef rather than targetRefs. It is deprecated upstream but still common
// in manifests in the wild, so the check has to follow it too.
func (s *DanglingPolicyTargetTestSuite) addPolicyWithDeprecatedTargetRef(ref gatewayv1.LocalPolicyTargetReferenceWithSectionName) {
	policy := s.newPolicy()
	//nolint:staticcheck // exercising the deprecated field is the point of this helper
	policy.Spec.TargetRef = &ref
	s.ctx.AddObject(policyName, policy)
}

func want(kind, name string) string {
	return fmt.Sprintf(
		"BackendTrafficPolicy %q targets %s %q in namespace %q, which is not present; "+
			"the policy attaches to nothing and its settings never apply",
		policyName, kind, name, namespace)
}

func (s *DanglingPolicyTargetTestSuite) expect(messages ...string) {
	diagnostics := make([]diagnostic.Diagnostic, 0, len(messages))
	for _, msg := range messages {
		diagnostics = append(diagnostics, diagnostic.Diagnostic{Message: msg})
	}
	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{policyName: diagnostics}},
	})
}

func (s *DanglingPolicyTargetTestSuite) TestFlagsATargetRefNamingAMissingRoute() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy(targetRef("HTTPRoute", "typo-route"))

	s.expect(want("HTTPRoute", "typo-route"))
}

func (s *DanglingPolicyTargetTestSuite) TestPassesWhenTheTargetRouteExists() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy(targetRef("HTTPRoute", "real-route"))

	s.expect()
}

func (s *DanglingPolicyTargetTestSuite) TestFlagsATargetRefNamingAMissingGateway() {
	s.addGateway()
	s.addPolicy(targetRef("Gateway", "typo-gateway"))

	s.expect(want("Gateway", "typo-gateway"))
}

func (s *DanglingPolicyTargetTestSuite) TestPassesWhenTheTargetGatewayExists() {
	s.addGateway()
	s.addPolicy(targetRef("Gateway", "real-gateway"))

	s.expect()
}

func (s *DanglingPolicyTargetTestSuite) TestFlagsAMissingListenerSet() {
	s.addListenerSet("real-listeners", namespace)
	s.addPolicy(targetRef("ListenerSet", "absent-listeners"))

	s.expect(want("ListenerSet", "absent-listeners"))
}

func (s *DanglingPolicyTargetTestSuite) TestPassesWhenTheTargetListenerSetExists() {
	s.addListenerSet("real-listeners", namespace)
	s.addPolicy(targetRef("ListenerSet", "real-listeners"))

	s.expect()
}

// Every kind Envoy Gateway permits as a target is resolved, not just HTTPRoute.
func (s *DanglingPolicyTargetTestSuite) TestAppliesToStreamRouteKinds() {
	s.addTCPRoute("real-stream-route", namespace)
	s.addPolicy(targetRef("TCPRoute", "typo-stream-route"))

	s.expect(want("TCPRoute", "typo-stream-route"))
}

// A route written as v1beta1 is the same object as one written as v1, so it
// satisfies the target and it also arms the guard for its kind.
func (s *DanglingPolicyTargetTestSuite) TestResolvesRoutesAtOtherServedVersions() {
	s.addBetaHTTPRoute("beta-route", namespace)
	s.addPolicy(targetRef("HTTPRoute", "beta-route"))

	s.expect()
}

func (s *DanglingPolicyTargetTestSuite) TestFollowsTheDeprecatedSingularTargetRef() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicyWithDeprecatedTargetRef(targetRef("HTTPRoute", "typo-route"))

	s.expect(want("HTTPRoute", "typo-route"))
}

func (s *DanglingPolicyTargetTestSuite) TestPassesWhenTheDeprecatedTargetRefResolves() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicyWithDeprecatedTargetRef(targetRef("HTTPRoute", "real-route"))

	s.expect()
}

// A route of the right name but the wrong kind does not satisfy the target.
func (s *DanglingPolicyTargetTestSuite) TestKindMustMatch() {
	s.addGateway()
	s.addHTTPRoute("shared-name", namespace)
	s.addPolicy(targetRef("Gateway", "shared-name"))

	s.expect(want("Gateway", "shared-name"))
}

// Linting a policy without the chart that defines its target is normal: the
// Gateway usually comes from a platform repo. With no object of the target
// kind in the namespace, the check has nothing to judge against and stays
// quiet rather than flagging every policy in the set.
func (s *DanglingPolicyTargetTestSuite) TestStaysQuietWhenTheSetHasNoObjectOfTheTargetKind() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy(targetRef("Gateway", "gateway-defined-elsewhere"))

	s.expect()
}

// The guard is per namespace, not per kind alone: a set can carry routes in
// its own namespace while saying nothing about another team's.
func (s *DanglingPolicyTargetTestSuite) TestGuardIsScopedToTheNamespace() {
	s.addHTTPRoute("someone-elses-route", "other-team")
	s.addPolicy(targetRef("HTTPRoute", "route-defined-elsewhere"))

	s.expect()
}

// A BackendTrafficPolicy targetRef carries no namespace, so a target of the
// same name in another namespace does not satisfy it.
func (s *DanglingPolicyTargetTestSuite) TestTargetResolvesOnlyInThePolicyNamespace() {
	s.addHTTPRoute("local-route", namespace)
	s.addHTTPRoute("shared-route", "other-team")
	s.addPolicy(targetRef("HTTPRoute", "shared-route"))

	s.expect(want("HTTPRoute", "shared-route"))
}

// A target in another implementation's API group is not ours to resolve, and
// Envoy Gateway's CRD rejects one anyway.
func (s *DanglingPolicyTargetTestSuite) TestIgnoresTargetsInOtherAPIGroups() {
	s.addHTTPRoute("real-route", namespace)
	ref := targetRef("HTTPRoute", "not-a-gateway-api-route")
	ref.Group = "some.vendor.io"
	s.addPolicy(ref)

	s.expect()
}

// An omitted group still means the Gateway API group here, since the CRD
// permits no other, so leaving it out must not open a hole.
func (s *DanglingPolicyTargetTestSuite) TestTreatsAnOmittedGroupAsGatewayAPI() {
	s.addHTTPRoute("real-route", namespace)
	ref := targetRef("HTTPRoute", "typo-route")
	ref.Group = ""
	s.addPolicy(ref)

	s.expect(want("HTTPRoute", "typo-route"))
}

// A kind outside the permitted target set is not something gwlint indexes, so
// its absence here says nothing about whether it exists.
func (s *DanglingPolicyTargetTestSuite) TestIgnoresKindsThatCannotBeTargets() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy(targetRef("Service", "checkout"))

	s.expect()
}

// A sectionName narrows which listener or rule the policy applies to. It says
// nothing about whether the target object itself resolves.
func (s *DanglingPolicyTargetTestSuite) TestSectionNameDoesNotAffectResolution() {
	s.addGateway()
	ref := targetRef("Gateway", "real-gateway")
	ref.SectionName = sectionPtr("https")
	s.addPolicy(ref)

	s.expect()
}

func (s *DanglingPolicyTargetTestSuite) TestReportsEveryDanglingTarget() {
	s.addGateway()
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy(
		targetRef("Gateway", "real-gateway"),
		targetRef("Gateway", "missing-gateway"),
		targetRef("HTTPRoute", "missing-route"),
	)

	s.expect(want("Gateway", "missing-gateway"), want("HTTPRoute", "missing-route"))
}

// The deprecated singular field and the plural one can both be set, and both
// are judged.
func (s *DanglingPolicyTargetTestSuite) TestWalksBothTargetFields() {
	s.addHTTPRoute("real-route", namespace)
	policy := s.newPolicy()
	//nolint:staticcheck // exercising the deprecated field is the point of this test
	policy.Spec.TargetRef = &gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: targetRef("HTTPRoute", "deprecated-typo").LocalPolicyTargetReference,
	}
	policy.Spec.TargetRefs = []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		targetRef("HTTPRoute", "plural-typo"),
	}
	s.ctx.AddObject(policyName, policy)

	s.expect(want("HTTPRoute", "deprecated-typo"), want("HTTPRoute", "plural-typo"))
}

// A targetSelector names nothing, so matching no route is not a dangling
// reference and belongs to a different check.
func (s *DanglingPolicyTargetTestSuite) TestIgnoresTargetSelectors() {
	s.addHTTPRoute("real-route", namespace)
	policy := s.newPolicy()
	policy.Spec.TargetSelectors = []egv1a1.TargetSelector{
		{Kind: "HTTPRoute", MatchLabels: map[string]string{"team": "nobody"}},
	}
	s.ctx.AddObject(policyName, policy)

	s.expect()
}

// A policy with no targets at all is nothing for this check to say.
func (s *DanglingPolicyTargetTestSuite) TestIgnoresAPolicyWithNoTargets() {
	s.addHTTPRoute("real-route", namespace)
	s.addPolicy()

	s.expect()
}
