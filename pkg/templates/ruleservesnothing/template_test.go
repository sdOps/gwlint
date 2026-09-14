package ruleservesnothing

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
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const namespace = "default"

func TestRuleServesNothing(t *testing.T) {
	suite.Run(t, new(RuleServesNothingTestSuite))
}

type RuleServesNothingTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *RuleServesNothingTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func namePtr(n gatewayv1.SectionName) *gatewayv1.SectionName { return &n }

func backend(name string) gatewayv1.BackendRef {
	return gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name)},
	}
}

func httpBackends(names ...string) []gatewayv1.HTTPBackendRef {
	out := make([]gatewayv1.HTTPBackendRef, 0, len(names))
	for _, name := range names {
		out = append(out, gatewayv1.HTTPBackendRef{BackendRef: backend(name)})
	}
	return out
}

func httpFilters(types ...gatewayv1.HTTPRouteFilterType) []gatewayv1.HTTPRouteFilter {
	out := make([]gatewayv1.HTTPRouteFilter, 0, len(types))
	for _, t := range types {
		out = append(out, gatewayv1.HTTPRouteFilter{Type: t})
	}
	return out
}

func (s *RuleServesNothingTestSuite) addHTTPRoute(name string, rules ...gatewayv1.HTTPRouteRule) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *RuleServesNothingTestSuite) addBetaHTTPRoute(name string, rules ...gatewayv1b1.HTTPRouteRule) {
	route := &gatewayv1b1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1b1.HTTPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: gatewayv1b1.GroupVersion.Version,
	}.WithKind("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *RuleServesNothingTestSuite) addGRPCRoute(name string, rules ...gatewayv1.GRPCRouteRule) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GRPCRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk("GRPCRoute"))
	s.ctx.AddObject(name, route)
}

func (s *RuleServesNothingTestSuite) addAlpha2TCPRoute(name string, rules ...gatewayv1a2.TCPRouteRule) {
	route := &gatewayv1a2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1a2.TCPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: gatewayv1a2.GroupVersion.Version,
	}.WithKind("TCPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *RuleServesNothingTestSuite) addService(name string) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	svc.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	s.ctx.AddObject(name, svc)
}

func want(routeKind, route, rule string) string {
	return fmt.Sprintf(
		"%s %q %s has no backendRefs and no filter that produces a response, "+
			"so requests matching the rule are answered with an error instead of being served",
		routeKind, route, rule)
}

func (s *RuleServesNothingTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *RuleServesNothingTestSuite) TestFlagsARuleWithOnlyAHeaderModifier() {
	s.addHTTPRoute("tracing-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(gatewayv1.HTTPRouteFilterRequestHeaderModifier),
	})

	s.expect(map[string][]string{
		"tracing-route": {want("HTTPRoute", "tracing-route", "rule 0")},
	})
}

func (s *RuleServesNothingTestSuite) TestFlagsARuleWithNeitherBackendsNorFilters() {
	s.addHTTPRoute("empty-route", gatewayv1.HTTPRouteRule{
		Matches: []gatewayv1.HTTPRouteMatch{{}},
	})

	s.expect(map[string][]string{
		"empty-route": {want("HTTPRoute", "empty-route", "rule 0")},
	})
}

func (s *RuleServesNothingTestSuite) TestPassesWhenTheRuleHasABackendRef() {
	s.addHTTPRoute("served-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends("served-api"),
	})

	s.expect(nil)
}

// A RequestRedirect answers the request itself, so the rule serves something
// without ever reaching a backend.
func (s *RuleServesNothingTestSuite) TestPassesOnARequestRedirect() {
	s.addHTTPRoute("redirect-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(gatewayv1.HTTPRouteFilterRequestRedirect),
	})

	s.expect(nil)
}

// An ExtensionRef hands the request to something gwlint cannot see into, so it
// counts as serving a response rather than being guessed at.
func (s *RuleServesNothingTestSuite) TestPassesOnAnExtensionRef() {
	s.addHTTPRoute("extension-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(gatewayv1.HTTPRouteFilterExtensionRef),
	})

	s.expect(nil)
}

// A terminating filter alongside others still clears the rule.
func (s *RuleServesNothingTestSuite) TestPassesWhenATerminatingFilterIsOneOfSeveral() {
	s.addHTTPRoute("header-redirect-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(
			gatewayv1.HTTPRouteFilterRequestHeaderModifier,
			gatewayv1.HTTPRouteFilterRequestRedirect,
		),
	})

	s.expect(nil)
}

// A URLRewrite rewrites a request that still has to reach a backend, so on its
// own it leaves the rule serving nothing.
func (s *RuleServesNothingTestSuite) TestURLRewriteDoesNotServeAResponse() {
	s.addHTTPRoute("rewrite-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(gatewayv1.HTTPRouteFilterURLRewrite),
	})

	s.expect(map[string][]string{
		"rewrite-route": {want("HTTPRoute", "rewrite-route", "rule 0")},
	})
}

// A mirror copies the request elsewhere and still forwards the original, which
// needs a backend to forward it to.
func (s *RuleServesNothingTestSuite) TestRequestMirrorDoesNotServeAResponse() {
	s.addHTTPRoute("mirror-route", gatewayv1.HTTPRouteRule{
		Name:    namePtr("shadow"),
		Filters: httpFilters(gatewayv1.HTTPRouteFilterRequestMirror),
	})

	s.expect(map[string][]string{
		"mirror-route": {want("HTTPRoute", "mirror-route", `rule "shadow"`)},
	})
}

func (s *RuleServesNothingTestSuite) TestJudgesEachRuleSeparately() {
	s.addHTTPRoute("mixed-route",
		gatewayv1.HTTPRouteRule{
			BackendRefs: httpBackends("mixed-live"),
		},
		gatewayv1.HTTPRouteRule{
			Name:    namePtr("legacy"),
			Filters: httpFilters(gatewayv1.HTTPRouteFilterResponseHeaderModifier),
		},
		gatewayv1.HTTPRouteRule{
			Filters: httpFilters(gatewayv1.HTTPRouteFilterRequestRedirect),
		},
		gatewayv1.HTTPRouteRule{},
	)

	s.expect(map[string][]string{
		"mixed-route": {
			want("HTTPRoute", "mixed-route", `rule "legacy"`),
			want("HTTPRoute", "mixed-route", "rule 3"),
		},
	})
}

// GRPCRoute has no RequestRedirect filter, so ExtensionRef is the only one of
// its filters that serves a response.
func (s *RuleServesNothingTestSuite) TestAppliesToGRPCRoutes() {
	s.addGRPCRoute("grpc-empty-route", gatewayv1.GRPCRouteRule{
		Filters: []gatewayv1.GRPCRouteFilter{{Type: gatewayv1.GRPCRouteFilterRequestHeaderModifier}},
	})
	s.addGRPCRoute("grpc-extension-route", gatewayv1.GRPCRouteRule{
		Filters: []gatewayv1.GRPCRouteFilter{{Type: gatewayv1.GRPCRouteFilterExtensionRef}},
	})

	s.expect(map[string][]string{
		"grpc-empty-route": {want("GRPCRoute", "grpc-empty-route", "rule 0")},
	})
}

// Stream rules carry no filters at all, so an empty backendRefs list is the
// whole of the question for them.
func (s *RuleServesNothingTestSuite) TestAppliesToStreamRoutes() {
	s.addAlpha2TCPRoute("tcp-empty-route", gatewayv1a2.TCPRouteRule{Name: namePtr("db")})

	s.expect(map[string][]string{
		"tcp-empty-route": {want("TCPRoute", "tcp-empty-route", `rule "db"`)},
	})
}

func (s *RuleServesNothingTestSuite) TestAppliesToV1beta1HTTPRoutes() {
	s.addBetaHTTPRoute("beta-route", gatewayv1b1.HTTPRouteRule{
		Filters: []gatewayv1b1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestHeaderModifier}},
	})

	s.expect(map[string][]string{
		"beta-route": {want("HTTPRoute", "beta-route", "rule 0")},
	})
}

// A route with no rules at all has nothing to report.
func (s *RuleServesNothingTestSuite) TestIgnoresARouteWithNoRules() {
	s.addHTTPRoute("ruleless-route")

	s.expect(nil)
}

// The check reads one route and nothing else, so a set holding only routes is
// judged exactly as a complete one would be.
func (s *RuleServesNothingTestSuite) TestNeedsNothingElseInTheManifestSet() {
	s.addHTTPRoute("lonely-route", gatewayv1.HTTPRouteRule{
		Filters: httpFilters(gatewayv1.HTTPRouteFilterRequestHeaderModifier),
	})

	s.expect(map[string][]string{
		"lonely-route": {want("HTTPRoute", "lonely-route", "rule 0")},
	})
}

func (s *RuleServesNothingTestSuite) TestIgnoresObjectsThatAreNotRoutes() {
	s.addService("some-service")

	s.expect(nil)
}
