package duplicaterulename

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	routeName = "partner-api-route"
	namespace = "default"
)

func TestDuplicateRuleName(t *testing.T) {
	suite.Run(t, new(DuplicateRuleNameTestSuite))
}

type DuplicateRuleNameTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DuplicateRuleNameTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: version}.WithKind(kind)
}

func namePtr(n gatewayv1.SectionName) *gatewayv1.SectionName { return &n }

// rule builds an HTTPRoute rule that forwards somewhere, so a finding is about
// the name rather than about the rule being empty.
func rule(name *gatewayv1.SectionName) gatewayv1.HTTPRouteRule {
	return gatewayv1.HTTPRouteRule{
		Name: name,
		BackendRefs: []gatewayv1.HTTPBackendRef{{
			BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{Name: "partner-api"},
			},
		}},
	}
}

func (s *DuplicateRuleNameTestSuite) addRoute(rules ...gatewayv1.HTTPRouteRule) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk(gatewayv1.GroupVersion.Version, "HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *DuplicateRuleNameTestSuite) addGRPCRoute(name string, ruleNames ...gatewayv1.SectionName) {
	rules := make([]gatewayv1.GRPCRouteRule, 0, len(ruleNames))
	for _, n := range ruleNames {
		rules = append(rules, gatewayv1.GRPCRouteRule{Name: namePtr(n)})
	}
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GRPCRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk(gatewayv1.GroupVersion.Version, "GRPCRoute"))
	s.ctx.AddObject(name, route)
}

func (s *DuplicateRuleNameTestSuite) addAlpha2TCPRoute(name string, ruleNames ...gatewayv1.SectionName) {
	rules := make([]gatewayv1a2.TCPRouteRule, 0, len(ruleNames))
	for _, n := range ruleNames {
		rules = append(rules, gatewayv1a2.TCPRouteRule{Name: namePtr(n)})
	}
	route := &gatewayv1a2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1a2.TCPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk(gatewayv1a2.GroupVersion.Version, "TCPRoute"))
	s.ctx.AddObject(name, route)
}

func want(routeKind, route string, count int, ruleName string) string {
	return fmt.Sprintf(
		"%s %q has %d rules named %q; a policy targetRef sectionName naming that rule "+
			"cannot address one of them unambiguously",
		routeKind, route, count, ruleName)
}

func (s *DuplicateRuleNameTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DuplicateRuleNameTestSuite) TestFlagsTwoRulesSharingAName() {
	s.addRoute(rule(namePtr("checkout")), rule(namePtr("checkout")))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, 2, "checkout")},
	})
}

func (s *DuplicateRuleNameTestSuite) TestPassesWhenEveryRuleNameIsUnique() {
	s.addRoute(rule(namePtr("checkout")), rule(namePtr("cart")))

	s.expect(nil)
}

// Naming a rule is optional and unnamed rules cannot be addressed by
// sectionName at all, so any number of them collide with nothing.
func (s *DuplicateRuleNameTestSuite) TestPassesWhenNoRuleIsNamed() {
	s.addRoute(rule(nil), rule(nil), rule(nil))

	s.expect(nil)
}

func (s *DuplicateRuleNameTestSuite) TestPassesWithASingleRule() {
	s.addRoute(rule(namePtr("checkout")))

	s.expect(nil)
}

func (s *DuplicateRuleNameTestSuite) TestPassesWithNoRulesAtAll() {
	s.addRoute()

	s.expect(nil)
}

// A name repeated three times is still one problem, and the count says how bad
// it is.
func (s *DuplicateRuleNameTestSuite) TestReportsARepeatedNameOnce() {
	s.addRoute(rule(namePtr("checkout")), rule(namePtr("checkout")), rule(namePtr("checkout")))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, 3, "checkout")},
	})
}

func (s *DuplicateRuleNameTestSuite) TestReportsEveryRepeatedNameOnARoute() {
	s.addRoute(
		rule(namePtr("checkout")),
		rule(namePtr("cart")),
		rule(namePtr("checkout")),
		rule(namePtr("cart")),
		rule(namePtr("search")),
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, 2, "checkout"),
			want("HTTPRoute", routeName, 2, "cart"),
		},
	})
}

// A rule that forwards nowhere still occupies its name, so rules have to be
// read from the spec rather than from the route's backendRefs.
func (s *DuplicateRuleNameTestSuite) TestCountsRulesThatHaveNoBackendRefs() {
	s.addRoute(
		gatewayv1.HTTPRouteRule{Name: namePtr("redirect")},
		rule(namePtr("redirect")),
	)

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, 2, "redirect")},
	})
}

// An empty name is schema-invalid; kubeconform reports it, and treating it as
// a duplicate here would just be a second, worse account of the same problem.
func (s *DuplicateRuleNameTestSuite) TestIgnoresEmptyRuleNames() {
	s.addRoute(rule(namePtr("")), rule(namePtr("")))

	s.expect(nil)
}

// Every route kind names its rules the same way, at every served version.
func (s *DuplicateRuleNameTestSuite) TestAppliesToOtherRouteKinds() {
	s.addGRPCRoute("grpc-route", "echo", "echo")
	s.addAlpha2TCPRoute("tcp-route", "primary", "primary")

	s.expect(map[string][]string{
		"grpc-route": {want("GRPCRoute", "grpc-route", 2, "echo")},
		"tcp-route":  {want("TCPRoute", "tcp-route", 2, "primary")},
	})
}

// Objects that are not routes share the context and must stay untouched.
func (s *DuplicateRuleNameTestSuite) TestIgnoresNonRouteObjects() {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "partner-api", Namespace: namespace}}
	svc.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	s.ctx.AddObject("partner-api", svc)
	s.addRoute(rule(namePtr("checkout")), rule(namePtr("checkout")))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, 2, "checkout")},
	})
}
