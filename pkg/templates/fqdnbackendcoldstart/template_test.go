package fqdnbackendcoldstart

import (
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

func TestFQDNBackendColdStart(t *testing.T) {
	suite.Run(t, new(FQDNBackendColdStartTestSuite))
}

type FQDNBackendColdStartTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *FQDNBackendColdStartTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind  { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group { return &g }

func (s *FQDNBackendColdStartTestSuite) addRoute(fqdnBackend bool) {
	s.addRouteWithParentGateway(fqdnBackend, "")
}

func (s *FQDNBackendColdStartTestSuite) addRouteWithParentGateway(fqdnBackend bool, parentGateway string) {
	var parentRefs []gatewayv1.ParentReference
	if parentGateway != "" {
		parentRefs = []gatewayv1.ParentReference{{Name: gatewayv1.ObjectName(parentGateway)}}
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "sso-route", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
			Rules: []gatewayv1.HTTPRouteRule{
				{
					BackendRefs: []gatewayv1.HTTPBackendRef{
						{
							BackendRef: gatewayv1.BackendRef{
								BackendObjectReference: gatewayv1.BackendObjectReference{
									Group: grpPtr(gatewayv1.Group(egv1a1.GroupName)),
									Kind:  kindPtr(gatewayv1.Kind(egv1a1.KindBackend)),
									Name:  "sso-upstream",
								},
							},
						},
					},
				},
			},
		},
	}
	route.SetGroupVersionKind(schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("HTTPRoute"))
	s.ctx.AddObject("sso-route", route)

	backend := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "sso-upstream", Namespace: "default"},
	}
	if fqdnBackend {
		backend.Spec.Endpoints = []egv1a1.BackendEndpoint{
			{FQDN: &egv1a1.FQDNEndpoint{Hostname: "sso.example-idp.com", Port: 443}},
		}
	} else {
		ip := "10.0.0.1"
		backend.Spec.Endpoints = []egv1a1.BackendEndpoint{
			{IP: &egv1a1.IPEndpoint{Address: ip, Port: 443}},
		}
	}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject("sso-upstream", backend)
}

func (s *FQDNBackendColdStartTestSuite) addPolicy(healthCheck *egv1a1.HealthCheck) {
	s.addPolicyTargeting(healthCheck, "HTTPRoute", "sso-route")
}

func (s *FQDNBackendColdStartTestSuite) addPolicyTargeting(healthCheck *egv1a1.HealthCheck, targetKind gatewayv1.Kind, targetName gatewayv1.ObjectName) {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: "sso-route-policy", Namespace: "default"},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{
				TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					{
						LocalPolicyTargetReference: gatewayv1.LocalPolicyTargetReference{
							Group: gatewayv1.GroupName,
							Kind:  targetKind,
							Name:  targetName,
						},
					},
				},
			},
			ClusterSettings: egv1a1.ClusterSettings{HealthCheck: healthCheck},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject("sso-route-policy", policy)
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsFQDNBackendWithNoHealthCheck() {
	s.addRoute(true)
	s.addPolicy(nil)

	s.Validate(s.ctx, []templates.TestCase{
		{
			Diagnostics: map[string][]diagnostic.Diagnostic{
				"sso-route-policy": {{
					Message: `BackendTrafficPolicy has no health check, but targets HTTPRoute "sso-route", ` +
						`which routes to Backend "sso-upstream" with FQDN endpoint "sso.example-idp.com"; ` +
						`this backend resolves via DNS at startup and can 503 before the name resolves`,
				}},
			},
		},
	})
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenHealthCheckConfigured() {
	s.addRoute(true)
	s.addPolicy(&egv1a1.HealthCheck{Passive: &egv1a1.PassiveHealthCheck{}})

	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{}},
	})
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendIsNotFQDN() {
	s.addRoute(false)
	s.addPolicy(nil)

	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{}},
	})
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsGatewayLevelPolicyCoveringFQDNRoute() {
	s.addRouteWithParentGateway(true, "gateway-public")
	s.addPolicyTargeting(nil, "Gateway", "gateway-public")

	s.Validate(s.ctx, []templates.TestCase{
		{
			Diagnostics: map[string][]diagnostic.Diagnostic{
				"sso-route-policy": {{
					Message: `BackendTrafficPolicy has no health check, but targets Gateway "gateway-public", ` +
						`whose attached HTTPRoute "sso-route", which routes to Backend "sso-upstream" with ` +
						`FQDN endpoint "sso.example-idp.com"; this backend resolves via DNS at startup and ` +
						`can 503 before the name resolves`,
				}},
			},
		},
	})
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenRouteNotAttachedToTargetedGateway() {
	s.addRouteWithParentGateway(true, "some-other-gateway")
	s.addPolicyTargeting(nil, "Gateway", "gateway-public")

	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{}},
	})
}
