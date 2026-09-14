package backendwithoutendpoints

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
)

const (
	backendName = "partner-api-upstream"
	namespace   = "default"
)

func TestBackendWithoutEndpoints(t *testing.T) {
	suite.Run(t, new(BackendWithoutEndpointsTestSuite))
}

type BackendWithoutEndpointsTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *BackendWithoutEndpointsTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func typePtr(t egv1a1.BackendType) *egv1a1.BackendType { return &t }

func (s *BackendWithoutEndpointsTestSuite) addBackend(name string, spec egv1a1.BackendSpec) {
	backend := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject(name, backend)
}

func want(name string) string {
	return fmt.Sprintf(
		"Backend %q is of type %q but declares no endpoints; "+
			"it programs a cluster with no members, so every backendRef resolving to it fails at the gateway",
		name, egv1a1.BackendTypeEndpoints)
}

func (s *BackendWithoutEndpointsTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// The shape the bug actually renders as: a values gap leaves spec empty, the
// CRD accepts it because endpoints is optional, and the Backend serves nothing.
func (s *BackendWithoutEndpointsTestSuite) TestFlagsABackendWithNoEndpointsBlock() {
	s.addBackend(backendName, egv1a1.BackendSpec{})

	s.expect(map[string][]string{backendName: {want(backendName)}})
}

func (s *BackendWithoutEndpointsTestSuite) TestFlagsAnEmptyEndpointsList() {
	s.addBackend(backendName, egv1a1.BackendSpec{Endpoints: []egv1a1.BackendEndpoint{}})

	s.expect(map[string][]string{backendName: {want(backendName)}})
}

func (s *BackendWithoutEndpointsTestSuite) TestPassesWithAnFQDNEndpoint() {
	s.addBackend(backendName, egv1a1.BackendSpec{
		Endpoints: []egv1a1.BackendEndpoint{
			{FQDN: &egv1a1.FQDNEndpoint{Hostname: "partner-api.example.com", Port: 443}},
		},
	})

	s.expect(nil)
}

func (s *BackendWithoutEndpointsTestSuite) TestPassesWithAnIPEndpoint() {
	s.addBackend(backendName, egv1a1.BackendSpec{
		Endpoints: []egv1a1.BackendEndpoint{
			{IP: &egv1a1.IPEndpoint{Address: "10.0.0.20", Port: 8080}},
		},
	})

	s.expect(nil)
}

func (s *BackendWithoutEndpointsTestSuite) TestPassesWithAUnixSocketEndpoint() {
	s.addBackend(backendName, egv1a1.BackendSpec{
		Endpoints: []egv1a1.BackendEndpoint{
			{Unix: &egv1a1.UnixSocket{Path: "/var/run/partner.sock"}},
		},
	})

	s.expect(nil)
}

// An explicit type of Endpoints is the CRD default spelled out, so it is
// judged exactly like an omitted one.
func (s *BackendWithoutEndpointsTestSuite) TestFlagsAnExplicitEndpointsTypeWithNoEndpoints() {
	s.addBackend(backendName, egv1a1.BackendSpec{Type: typePtr(egv1a1.BackendTypeEndpoints)})

	s.expect(map[string][]string{backendName: {want(backendName)}})
}

// A DynamicResolver Backend resolves its upstream from the request's Host
// header. The CRD forbids it from carrying endpoints, so an empty list is the
// only valid shape and flagging it would be a false positive.
func (s *BackendWithoutEndpointsTestSuite) TestPassesForADynamicResolverBackend() {
	s.addBackend(backendName, egv1a1.BackendSpec{Type: typePtr(egv1a1.BackendTypeDynamicResolver)})

	s.expect(nil)
}

// Other settings on the spec do not give the Backend anywhere to send traffic.
func (s *BackendWithoutEndpointsTestSuite) TestFlagsABackendThatOnlyConfiguresProtocols() {
	s.addBackend(backendName, egv1a1.BackendSpec{
		AppProtocols: []egv1a1.AppProtocolType{egv1a1.AppProtocolTypeH2C},
	})

	s.expect(map[string][]string{backendName: {want(backendName)}})
}

func (s *BackendWithoutEndpointsTestSuite) TestReportsEachEmptyBackendSeparately() {
	s.addBackend("first-upstream", egv1a1.BackendSpec{})
	s.addBackend("second-upstream", egv1a1.BackendSpec{})
	s.addBackend("third-upstream", egv1a1.BackendSpec{
		Endpoints: []egv1a1.BackendEndpoint{
			{IP: &egv1a1.IPEndpoint{Address: "10.0.0.21", Port: 8080}},
		},
	})

	s.expect(map[string][]string{
		"first-upstream":  {want("first-upstream")},
		"second-upstream": {want("second-upstream")},
	})
}

// The check is scoped to Backend, but the engine hands every object in the
// context to every check func, so a non-Backend must fall straight through.
func (s *BackendWithoutEndpointsTestSuite) TestIgnoresNonBackendObjects() {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: "some-route", Namespace: namespace}}
	route.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version,
	}.WithKind("HTTPRoute"))
	s.ctx.AddObject("some-route", route)

	s.expect(nil)
}
