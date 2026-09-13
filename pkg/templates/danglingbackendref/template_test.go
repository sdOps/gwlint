package danglingbackendref

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
)

const (
	routeName = "checkout-route"
	namespace = "shop"
)

func TestDanglingBackendRef(t *testing.T) {
	suite.Run(t, new(DanglingBackendRefTestSuite))
}

type DanglingBackendRefTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DanglingBackendRefTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }

func (s *DanglingBackendRefTestSuite) addService(name, ns string) {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	svc.SetGroupVersionKind(schema.GroupVersion{Version: "v1"}.WithKind("Service"))
	s.ctx.AddObject("svc-"+ns+"-"+name, svc)
}

func (s *DanglingBackendRefTestSuite) addBackend(name, ns string) {
	backend := &egv1a1.Backend{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject("backend-"+ns+"-"+name, backend)
}

func serviceRef(name, ns string) gatewayv1.BackendObjectReference {
	ref := gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name)}
	if ns != "" {
		ref.Namespace = nsPtr(gatewayv1.Namespace(ns))
	}
	return ref
}

func envoyBackendRef(name string) gatewayv1.BackendObjectReference {
	return gatewayv1.BackendObjectReference{
		Group: grpPtr(gatewayv1.Group(egv1a1.GroupName)),
		Kind:  kindPtr(gatewayv1.Kind(egv1a1.KindBackend)),
		Name:  gatewayv1.ObjectName(name),
	}
}

func (s *DanglingBackendRefTestSuite) addRoute(refs ...gatewayv1.BackendObjectReference) {
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
	route.SetGroupVersionKind(
		schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func want(kind, name string) string {
	return fmt.Sprintf(
		"HTTPRoute %q routes to %s %q in namespace %q, which is not present; "+
			"requests matching that rule have nowhere to go", routeName, kind, name, namespace)
}

func (s *DanglingBackendRefTestSuite) expect(messages ...string) {
	diagnostics := make([]diagnostic.Diagnostic, 0, len(messages))
	for _, m := range messages {
		diagnostics = append(diagnostics, diagnostic.Diagnostic{Message: m})
	}
	expected := map[string][]diagnostic.Diagnostic{}
	if len(diagnostics) > 0 {
		expected[routeName] = diagnostics
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DanglingBackendRefTestSuite) TestFlagsAMisspeltService() {
	s.addService("checkout", namespace)
	s.addRoute(serviceRef("checkuot", ""))

	s.expect(want("Service", "checkuot"))
}

func (s *DanglingBackendRefTestSuite) TestPassesWhenTheServiceExists() {
	s.addService("checkout", namespace)
	s.addRoute(serviceRef("checkout", ""))

	s.expect()
}

// An omitted backendRef namespace means the route's own, per Gateway API.
func (s *DanglingBackendRefTestSuite) TestBackendRefDefaultsToTheRouteNamespace() {
	s.addService("checkout", "elsewhere")
	s.addService("unrelated", namespace)
	s.addRoute(serviceRef("checkout", ""))

	s.expect(want("Service", "checkout"))
}

func (s *DanglingBackendRefTestSuite) TestResolvesAnExplicitNamespace() {
	s.addService("checkout", "elsewhere")
	s.addRoute(serviceRef("checkout", "elsewhere"))

	s.expect()
}

func (s *DanglingBackendRefTestSuite) TestResolvesEnvoyGatewayBackends() {
	s.addBackend("partner-api", namespace)
	s.addRoute(envoyBackendRef("partner-api"))

	s.expect()
}

func (s *DanglingBackendRefTestSuite) TestFlagsAMissingEnvoyGatewayBackend() {
	s.addBackend("partner-api", namespace)
	s.addRoute(envoyBackendRef("partnr-api"))

	s.expect(want(egv1a1.KindBackend, "partnr-api"))
}

// A Service does not satisfy a ref that asked for a Backend.
func (s *DanglingBackendRefTestSuite) TestKindMustMatch() {
	s.addService("shared-name", namespace)
	s.addBackend("something-else", namespace)
	s.addRoute(envoyBackendRef("shared-name"))

	s.expect(want(egv1a1.KindBackend, "shared-name"))
}

// The guard that matters: a manifest set describing one namespace says nothing
// about references into another, which is the normal case when routes point at
// workloads owned by another team and rendered from another repo.
func (s *DanglingBackendRefTestSuite) TestStaysQuietForNamespacesTheSetDoesNotDescribe() {
	s.addService("envoy-controller", "gateway")
	s.addRoute(serviceRef("checkout", "production-blue"))

	s.expect()
}

// And the weaker guard is not enough on its own: a Service existing somewhere
// must not license judging every namespace.
func (s *DanglingBackendRefTestSuite) TestJudgesOnlyTheNamespacesDescribed() {
	s.addService("envoy-controller", "gateway")
	s.addService("checkout", namespace)
	s.addRoute(serviceRef("checkuot", ""), serviceRef("anything", "production-blue"))

	s.expect(want("Service", "checkuot"))
}

func (s *DanglingBackendRefTestSuite) TestStaysQuietWhenNothingResolvableWasPassed() {
	s.addRoute(serviceRef("checkout", ""))

	s.expect()
}

// A ref into another vendor's CRD is not something gwlint can judge: its
// absence here says nothing about whether it exists in a cluster.
func (s *DanglingBackendRefTestSuite) TestIgnoresUnknownBackendKinds() {
	s.addService("checkout", namespace)
	s.addRoute(gatewayv1.BackendObjectReference{
		Group: grpPtr("some.vendor.io"),
		Kind:  kindPtr("MeshBackend"),
		Name:  "elsewhere",
	})

	s.expect()
}

func (s *DanglingBackendRefTestSuite) TestReportsEveryDanglingRefOnARoute() {
	s.addService("checkout", namespace)
	s.addRoute(serviceRef("checkout", ""), serviceRef("missing-one", ""), serviceRef("missing-two", ""))

	s.expect(want("Service", "missing-one"), want("Service", "missing-two"))
}
