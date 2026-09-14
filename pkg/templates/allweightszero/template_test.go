package allweightszero

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

func TestAllWeightsZero(t *testing.T) {
	suite.Run(t, new(AllWeightsZeroTestSuite))
}

type AllWeightsZeroTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *AllWeightsZeroTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func alpha2GVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1a2.GroupVersion.Version}.WithKind(kind)
}

func weight(w int32) *int32 { return &w }

func namePtr(n gatewayv1.SectionName) *gatewayv1.SectionName { return &n }

// backend builds a backendRef with the given weight, where a nil weight is the
// omitted field that Gateway API defaults to 1.
func backend(name string, w *int32) gatewayv1.BackendRef {
	return gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name)},
		Weight:                 w,
	}
}

func httpBackends(refs ...gatewayv1.BackendRef) []gatewayv1.HTTPBackendRef {
	out := make([]gatewayv1.HTTPBackendRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, gatewayv1.HTTPBackendRef{BackendRef: ref})
	}
	return out
}

func (s *AllWeightsZeroTestSuite) addHTTPRoute(name string, rules ...gatewayv1.HTTPRouteRule) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addBetaHTTPRoute(name string, rules ...gatewayv1b1.HTTPRouteRule) {
	route := &gatewayv1b1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1b1.HTTPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: gatewayv1b1.GroupVersion.Version,
	}.WithKind("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addGRPCRoute(name string, rules ...gatewayv1.GRPCRouteRule) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GRPCRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk("GRPCRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addTCPRoute(name string, rules ...gatewayv1.TCPRouteRule) {
	route := &gatewayv1.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.TCPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(gvk("TCPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addAlpha2TLSRoute(name string, rules ...gatewayv1a2.TLSRouteRule) {
	route := &gatewayv1a2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1a2.TLSRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(alpha2GVK("TLSRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addAlpha2UDPRoute(name string, rules ...gatewayv1a2.UDPRouteRule) {
	route := &gatewayv1a2.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1a2.UDPRouteSpec{Rules: rules},
	}
	route.SetGroupVersionKind(alpha2GVK("UDPRoute"))
	s.ctx.AddObject(name, route)
}

func (s *AllWeightsZeroTestSuite) addService(name string) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	svc.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	s.ctx.AddObject(name, svc)
}

func want(routeKind, route, rule string, backends int) string {
	return fmt.Sprintf(
		"%s %q %s sets weight 0 on every one of its %d backendRefs, "+
			"so requests matching the rule are forwarded to none of them",
		routeKind, route, rule, backends)
}

func (s *AllWeightsZeroTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *AllWeightsZeroTestSuite) TestFlagsARuleWhoseBackendRefsAreAllWeightedZero() {
	s.addHTTPRoute("checkout-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("checkout-v1", weight(0)), backend("checkout-v2", weight(0))),
	})

	s.expect(map[string][]string{
		"checkout-route": {want("HTTPRoute", "checkout-route", "rule 0", 2)},
	})
}

func (s *AllWeightsZeroTestSuite) TestFlagsASingleBackendRefWeightedZero() {
	s.addHTTPRoute("single-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("single-api", weight(0))),
	})

	s.expect(map[string][]string{
		"single-route": {want("HTTPRoute", "single-route", "rule 0", 1)},
	})
}

func (s *AllWeightsZeroTestSuite) TestPassesWhenOneBackendStillTakesTraffic() {
	s.addHTTPRoute("shifting-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("shifting-v1", weight(100)), backend("shifting-v2", weight(0))),
	})

	s.expect(nil)
}

// An omitted weight defaults to 1, so a nil weight alongside an explicit 0 is a
// backend that does take traffic.
func (s *AllWeightsZeroTestSuite) TestOmittedWeightIsNotZero() {
	s.addHTTPRoute("default-weight-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("default-primary", nil), backend("default-canary", weight(0))),
	})

	s.expect(nil)
}

func (s *AllWeightsZeroTestSuite) TestPassesWhenEveryWeightIsOmitted() {
	s.addHTTPRoute("plain-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("plain-api", nil)),
	})

	s.expect(nil)
}

// A rule with no backendRefs at all serves nothing for a different reason, and
// rule-serves-nothing reports that one.
func (s *AllWeightsZeroTestSuite) TestIgnoresARuleWithNoBackendRefs() {
	s.addHTTPRoute("redirect-route", gatewayv1.HTTPRouteRule{
		Filters: []gatewayv1.HTTPRouteFilter{{Type: gatewayv1.HTTPRouteFilterRequestRedirect}},
	})

	s.expect(nil)
}

func (s *AllWeightsZeroTestSuite) TestJudgesEachRuleSeparately() {
	s.addHTTPRoute("mixed-route",
		gatewayv1.HTTPRouteRule{
			BackendRefs: httpBackends(backend("mixed-live", weight(10))),
		},
		gatewayv1.HTTPRouteRule{
			Name:        namePtr("retired"),
			BackendRefs: httpBackends(backend("mixed-old", weight(0)), backend("mixed-older", weight(0))),
		},
		gatewayv1.HTTPRouteRule{
			BackendRefs: httpBackends(backend("mixed-drained", weight(0))),
		},
	)

	s.expect(map[string][]string{
		"mixed-route": {
			want("HTTPRoute", "mixed-route", `rule "retired"`, 2),
			want("HTTPRoute", "mixed-route", "rule 2", 1),
		},
	})
}

func (s *AllWeightsZeroTestSuite) TestAppliesToGRPCRoutes() {
	s.addGRPCRoute("grpc-route", gatewayv1.GRPCRouteRule{
		BackendRefs: []gatewayv1.GRPCBackendRef{{BackendRef: backend("grpc-api", weight(0))}},
	})

	s.expect(map[string][]string{
		"grpc-route": {want("GRPCRoute", "grpc-route", "rule 0", 1)},
	})
}

func (s *AllWeightsZeroTestSuite) TestAppliesToTCPRoutes() {
	s.addTCPRoute("tcp-route", gatewayv1.TCPRouteRule{
		BackendRefs: []gatewayv1.BackendRef{backend("tcp-db", weight(0))},
	})

	s.expect(map[string][]string{
		"tcp-route": {want("TCPRoute", "tcp-route", "rule 0", 1)},
	})
}

// The stream route kinds are usually authored as v1alpha2, whose rule types are
// their own, so those versions have to be read too.
func (s *AllWeightsZeroTestSuite) TestAppliesToAlpha2StreamRoutes() {
	s.addAlpha2TLSRoute("tls-route", gatewayv1a2.TLSRouteRule{
		Name:        namePtr("passthrough"),
		BackendRefs: []gatewayv1.BackendRef{backend("tls-broker", weight(0))},
	})
	s.addAlpha2UDPRoute("udp-route", gatewayv1a2.UDPRouteRule{
		BackendRefs: []gatewayv1.BackendRef{backend("udp-collector", weight(0))},
	})

	s.expect(map[string][]string{
		"tls-route": {want("TLSRoute", "tls-route", `rule "passthrough"`, 1)},
		"udp-route": {want("UDPRoute", "udp-route", "rule 0", 1)},
	})
}

func (s *AllWeightsZeroTestSuite) TestAppliesToV1beta1HTTPRoutes() {
	s.addBetaHTTPRoute("beta-route", gatewayv1b1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("beta-api", weight(0))),
	})

	s.expect(map[string][]string{
		"beta-route": {want("HTTPRoute", "beta-route", "rule 0", 1)},
	})
}

// The check reads one route and nothing else, so a set holding only routes is
// judged exactly as a complete one would be.
func (s *AllWeightsZeroTestSuite) TestNeedsNothingElseInTheManifestSet() {
	s.addHTTPRoute("lonely-route", gatewayv1.HTTPRouteRule{
		BackendRefs: httpBackends(backend("lonely-api", weight(0))),
	})

	s.expect(map[string][]string{
		"lonely-route": {want("HTTPRoute", "lonely-route", "rule 0", 1)},
	})
}

func (s *AllWeightsZeroTestSuite) TestIgnoresObjectsThatAreNotRoutes() {
	s.addService("some-service")

	s.expect(nil)
}
