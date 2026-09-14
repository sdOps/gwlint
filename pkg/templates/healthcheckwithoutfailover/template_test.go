package healthcheckwithoutfailover

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
	policyName  = "checkout-policy"
	routeName   = "checkout-route"
	backendName = "checkout-upstream"
	namespace   = "default"
)

func TestHealthCheckWithoutFailover(t *testing.T) {
	suite.Run(t, new(HealthCheckWithoutFailoverTestSuite))
}

type HealthCheckWithoutFailoverTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *HealthCheckWithoutFailoverTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}
func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func typePtr(t egv1a1.BackendType) *egv1a1.BackendType { return &t }

func envoyBackendRef(name string) gatewayv1.BackendObjectReference {
	return gatewayv1.BackendObjectReference{
		Group: grpPtr(gatewayv1.Group(egv1a1.GroupName)),
		Kind:  kindPtr(gatewayv1.Kind(egv1a1.KindBackend)),
		Name:  gatewayv1.ObjectName(name),
	}
}

func serviceRef(name string) gatewayv1.BackendObjectReference {
	return gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name)}
}

func (s *HealthCheckWithoutFailoverTestSuite) addRoute(refs ...gatewayv1.BackendObjectReference) {
	backendRefs := make([]gatewayv1.HTTPBackendRef, 0, len(refs))
	for _, ref := range refs {
		backendRefs = append(backendRefs, gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{BackendObjectReference: ref}})
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: backendRefs}}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *HealthCheckWithoutFailoverTestSuite) addBackend(name string, endpoints ...egv1a1.BackendEndpoint) {
	backend := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       egv1a1.BackendSpec{Endpoints: endpoints},
	}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject(name, backend)
}

func (s *HealthCheckWithoutFailoverTestSuite) addDynamicResolverBackend(name string) {
	backend := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       egv1a1.BackendSpec{Type: typePtr(egv1a1.BackendTypeDynamicResolver)},
	}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject(name, backend)
}

func (s *HealthCheckWithoutFailoverTestSuite) addPolicy(healthCheck *egv1a1.HealthCheck, targetKind gatewayv1.Kind, targetName gatewayv1.ObjectName) {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: namespace},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{
				TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.GroupName, Kind: targetKind, Name: targetName,
					}},
				},
			},
			ClusterSettings: egv1a1.ClusterSettings{HealthCheck: healthCheck},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject(policyName, policy)
}

func passiveHealthCheck() *egv1a1.HealthCheck {
	return &egv1a1.HealthCheck{Passive: &egv1a1.PassiveHealthCheck{}}
}

func (s *HealthCheckWithoutFailoverTestSuite) expect(messages ...string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for _, m := range messages {
		expected[policyName] = append(expected[policyName], diagnostic.Diagnostic{Message: m})
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func want(hostname string) string {
	return fmt.Sprintf(
		"BackendTrafficPolicy configures a health check and targets HTTPRoute %q, which routes to "+
			"Backend %q, whose only endpoint is FQDN %q on port 443; ejecting it empties the load "+
			"balancing set, so requests fail fast instead of failing over",
		routeName, backendName, hostname)
}

func (s *HealthCheckWithoutFailoverTestSuite) TestFlagsAHealthCheckOnASingleEndpointBackend() {
	s.addRoute(envoyBackendRef(backendName))
	s.addBackend(backendName, egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "a.example.com", Port: 443}})
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect(want("a.example.com"))
}

// No health check at all is fqdn-backend-cold-start's finding, not this one.
func (s *HealthCheckWithoutFailoverTestSuite) TestPassesWithNoHealthCheck() {
	s.addRoute(envoyBackendRef(backendName))
	s.addBackend(backendName, egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "a.example.com", Port: 443}})
	s.addPolicy(nil, "HTTPRoute", routeName)

	s.expect()
}

func (s *HealthCheckWithoutFailoverTestSuite) TestPassesWithTwoEndpoints() {
	s.addRoute(envoyBackendRef(backendName))
	s.addBackend(backendName,
		egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "a.example.com", Port: 443}},
		egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "b.example.com", Port: 443}},
	)
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect()
}

// More than one backendRef means other destinations exist, whether or not
// Envoy actually shifts traffic onto them depends on weights and retries this
// check does not model, so it says nothing rather than guessing.
func (s *HealthCheckWithoutFailoverTestSuite) TestPassesWithTwoBackendRefs() {
	s.addRoute(envoyBackendRef(backendName), envoyBackendRef("fallback-upstream"))
	s.addBackend(backendName, egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "a.example.com", Port: 443}})
	s.addBackend("fallback-upstream", egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "b.example.com", Port: 443}})
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect()
}

// A route to a core Service is not something gwlint can count endpoints for.
func (s *HealthCheckWithoutFailoverTestSuite) TestPassesForServiceBackends() {
	s.addRoute(serviceRef("checkout-service"))
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect()
}

// A DynamicResolver Backend has no endpoints to eject at all.
func (s *HealthCheckWithoutFailoverTestSuite) TestPassesForDynamicResolverBackends() {
	s.addRoute(envoyBackendRef(backendName))
	s.addDynamicResolverBackend(backendName)
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect()
}

func (s *HealthCheckWithoutFailoverTestSuite) TestPassesWhenTheBackendIsNotInTheSet() {
	s.addRoute(envoyBackendRef("undefined-upstream"))
	s.addPolicy(passiveHealthCheck(), "HTTPRoute", routeName)

	s.expect()
}

// A route-level policy displaces an inherited Gateway-level one entirely, so
// the Gateway-level policy's health check never actually applies here.
func (s *HealthCheckWithoutFailoverTestSuite) TestGatewayPolicyIsOverriddenByARoutePolicy() {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{Name: "edge-gateway"}}},
			Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: []gatewayv1.HTTPBackendRef{
				{BackendRef: gatewayv1.BackendRef{BackendObjectReference: envoyBackendRef(backendName)}},
			}}},
		},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
	s.addBackend(backendName, egv1a1.BackendEndpoint{FQDN: &egv1a1.FQDNEndpoint{Hostname: "a.example.com", Port: 443}})
	s.addPolicy(passiveHealthCheck(), "Gateway", "edge-gateway")

	otherPolicy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "route-policy", Namespace: namespace},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{
				TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					{LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
						Group: gatewayv1.GroupName, Kind: "HTTPRoute", Name: routeName,
					}},
				},
			},
		},
	}
	otherPolicy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject("route-policy", otherPolicy)

	s.expect()
}
