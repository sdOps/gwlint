package servicebackendwithoutport

import (
	"fmt"
	"testing"

	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
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

func TestServiceBackendWithoutPort(t *testing.T) {
	suite.Run(t, new(ServiceBackendWithoutPortTestSuite))
}

type ServiceBackendWithoutPortTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *ServiceBackendWithoutPortTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: version}.WithKind(kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func portPtr(p gatewayv1.PortNumber) *gatewayv1.PortNumber {
	return &p
}

func (s *ServiceBackendWithoutPortTestSuite) addRoute(refs ...gatewayv1.BackendObjectReference) {
	backendRefs := make([]gatewayv1.HTTPBackendRef, 0, len(refs))
	for _, ref := range refs {
		backendRefs = append(backendRefs, gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{BackendObjectReference: ref},
		})
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: backendRefs}}},
	}
	route.SetGroupVersionKind(gvk(gatewayv1.GroupVersion.Version, "HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *ServiceBackendWithoutPortTestSuite) addAlpha2TCPRoute(name string, ref gatewayv1.BackendObjectReference) {
	route := &gatewayv1a2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gatewayv1a2.TCPRouteSpec{Rules: []gatewayv1a2.TCPRouteRule{{
			BackendRefs: []gatewayv1.BackendRef{{BackendObjectReference: ref}},
		}}},
	}
	route.SetGroupVersionKind(gvk(gatewayv1a2.GroupVersion.Version, "TCPRoute"))
	s.ctx.AddObject(name, route)
}

func want(routeKind, route, service, ns string) string {
	return fmt.Sprintf(
		"%s %q routes to Service %q in namespace %q with no port; Gateway API requires a port "+
			"on a Service backendRef, so the reference does not resolve and the rule serves errors",
		routeKind, route, service, ns)
}

func (s *ServiceBackendWithoutPortTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// An omitted group and kind mean a core Service, which is how most Service
// backendRefs are written.
func (s *ServiceBackendWithoutPortTestSuite) TestFlagsAServiceBackendRefWithNoPort() {
	s.addRoute(gatewayv1.BackendObjectReference{Name: "partner-api"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "partner-api", namespace)},
	})
}

func (s *ServiceBackendWithoutPortTestSuite) TestPassesWhenThePortIsSet() {
	s.addRoute(gatewayv1.BackendObjectReference{Name: "partner-api", Port: portPtr(8080)})

	s.expect(nil)
}

func (s *ServiceBackendWithoutPortTestSuite) TestFlagsAnExplicitlySpelledServiceRef() {
	s.addRoute(gatewayv1.BackendObjectReference{
		Group: grpPtr(""), Kind: kindPtr("Service"), Name: "partner-api",
	})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "partner-api", namespace)},
	})
}

// A backendRef with no namespace resolves in the route's own namespace, which
// is what the message has to say for the reader to find the Service.
func (s *ServiceBackendWithoutPortTestSuite) TestReportsTheRefNamespace() {
	s.addRoute(gatewayv1.BackendObjectReference{Name: "partner-api", Namespace: nsPtr("payments")})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "partner-api", "payments")},
	})
}

// An Envoy Gateway Backend carries its own endpoint ports, so a ref to one
// needs no port here.
func (s *ServiceBackendWithoutPortTestSuite) TestIgnoresEnvoyGatewayBackendRefs() {
	s.addRoute(gatewayv1.BackendObjectReference{
		Group: grpPtr(egv1a1.GroupName), Kind: kindPtr(egv1a1.KindBackend), Name: "partner-api-upstream",
	})

	s.expect(nil)
}

// Another vendor's CRD sets its own rules about ports, and Gateway API's
// requirement does not reach it.
func (s *ServiceBackendWithoutPortTestSuite) TestIgnoresOtherVendorBackendKinds() {
	s.addRoute(gatewayv1.BackendObjectReference{
		Group: grpPtr("some.vendor.io"), Kind: kindPtr("ExternalService"), Name: "partner-api-upstream",
	})

	s.expect(nil)
}

// A Service in a group other than core is somebody else's Service-shaped CRD.
func (s *ServiceBackendWithoutPortTestSuite) TestIgnoresAServiceKindInAnotherGroup() {
	s.addRoute(gatewayv1.BackendObjectReference{
		Group: grpPtr("serving.knative.dev"), Kind: kindPtr("Service"), Name: "partner-api-knative",
	})

	s.expect(nil)
}

func (s *ServiceBackendWithoutPortTestSuite) TestReportsEveryPortlessRefOnARoute() {
	s.addRoute(
		gatewayv1.BackendObjectReference{Name: "partner-api", Port: portPtr(8080)},
		gatewayv1.BackendObjectReference{Name: "partner-api-canary"},
		gatewayv1.BackendObjectReference{Name: "partner-api-legacy"},
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, "partner-api-canary", namespace),
			want("HTTPRoute", routeName, "partner-api-legacy", namespace),
		},
	})
}

// Every route kind carries backendRefs the same way, at every served version.
func (s *ServiceBackendWithoutPortTestSuite) TestAppliesToOtherRouteKinds() {
	s.addAlpha2TCPRoute("tcp-route", gatewayv1.BackendObjectReference{Name: "database"})

	s.expect(map[string][]string{
		"tcp-route": {want("TCPRoute", "tcp-route", "database", namespace)},
	})
}

// The check reads the route alone, so whether the Service is in the lint run
// changes nothing: a portless ref is wrong with the Service present, and a
// ref that sets its port is fine with the Service absent.
func (s *ServiceBackendWithoutPortTestSuite) TestVerdictDoesNotDependOnTheServiceBeingPresent() {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "partner-api", Namespace: namespace}}
	svc.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Service"))
	s.ctx.AddObject("partner-api", svc)
	s.addRoute(gatewayv1.BackendObjectReference{Name: "partner-api"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "partner-api", namespace)},
	})
}

func (s *ServiceBackendWithoutPortTestSuite) TestPassesWithNoBackendRefsAtAll() {
	s.addRoute()

	s.expect(nil)
}
