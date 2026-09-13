package fqdnbackendcoldstart

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
	policyName  = "partner-api-route-policy"
	routeName   = "partner-api-route"
	backendName = "partner-api-upstream"
	fqdnHost    = "partner-api.example.com"
	namespace   = "default"
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

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func routeGVK(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

// envoyBackendRef builds a backendRef pointing at an Envoy Gateway Backend.
// Pass an empty namespace to leave it unset, meaning the route's own.
func envoyBackendRef(name, backendNamespace string) gatewayv1.BackendObjectReference {
	ref := gatewayv1.BackendObjectReference{
		Group: grpPtr(gatewayv1.Group(egv1a1.GroupName)),
		Kind:  kindPtr(gatewayv1.Kind(egv1a1.KindBackend)),
		Name:  gatewayv1.ObjectName(name),
	}
	if backendNamespace != "" {
		ref.Namespace = nsPtr(gatewayv1.Namespace(backendNamespace))
	}
	return ref
}

// serviceBackendRef builds a backendRef with no group/kind override, which
// Gateway API defaults to a core Service rather than an Envoy Gateway Backend.
func serviceBackendRef(name string) gatewayv1.BackendObjectReference {
	return gatewayv1.BackendObjectReference{Name: gatewayv1.ObjectName(name)}
}

func parentRefsFor(parentGateway string) []gatewayv1.ParentReference {
	if parentGateway == "" {
		return nil
	}
	return []gatewayv1.ParentReference{{Name: gatewayv1.ObjectName(parentGateway)}}
}

func (s *FQDNBackendColdStartTestSuite) addHTTPRoute(parentGateway string, refs ...gatewayv1.BackendObjectReference) {
	s.addHTTPRouteWithParentRefs(parentRefsFor(parentGateway), refs...)
}

func (s *FQDNBackendColdStartTestSuite) addHTTPRouteWithParentRefs(parentRefs []gatewayv1.ParentReference, refs ...gatewayv1.BackendObjectReference) {
	backendRefs := make([]gatewayv1.HTTPBackendRef, 0, len(refs))
	for _, ref := range refs {
		backendRefs = append(backendRefs, gatewayv1.HTTPBackendRef{
			BackendRef: gatewayv1.BackendRef{BackendObjectReference: ref},
		})
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
			Rules:           []gatewayv1.HTTPRouteRule{{BackendRefs: backendRefs}},
		},
	}
	route.SetGroupVersionKind(routeGVK("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *FQDNBackendColdStartTestSuite) addGRPCRoute(name, parentGateway string, refs ...gatewayv1.BackendObjectReference) {
	backendRefs := make([]gatewayv1.GRPCBackendRef, 0, len(refs))
	for _, ref := range refs {
		backendRefs = append(backendRefs, gatewayv1.GRPCBackendRef{
			BackendRef: gatewayv1.BackendRef{BackendObjectReference: ref},
		})
	}
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gatewayv1.GRPCRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefsFor(parentGateway)},
			Rules:           []gatewayv1.GRPCRouteRule{{BackendRefs: backendRefs}},
		},
	}
	route.SetGroupVersionKind(routeGVK("GRPCRoute"))
	s.ctx.AddObject(name, route)
}

// addBackend adds an Envoy Gateway Backend whose single endpoint is either an
// FQDN (the cold-start risk this check looks for) or a static IP.
func (s *FQDNBackendColdStartTestSuite) addBackend(name, backendNamespace string, fqdn bool) {
	backend := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: backendNamespace},
	}
	if fqdn {
		backend.Spec.Endpoints = []egv1a1.BackendEndpoint{
			{FQDN: &egv1a1.FQDNEndpoint{Hostname: fqdnHost, Port: 443}},
		}
	} else {
		backend.Spec.Endpoints = []egv1a1.BackendEndpoint{
			{IP: &egv1a1.IPEndpoint{Address: "10.0.0.1", Port: 443}},
		}
	}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject(name, backend)
}

// addRoute is the common setup: one HTTPRoute pointing at one Backend that
// either does or does not carry an FQDN endpoint.
func (s *FQDNBackendColdStartTestSuite) addRoute(fqdnBackend bool) {
	s.addRouteWithParentGateway(fqdnBackend, "")
}

func (s *FQDNBackendColdStartTestSuite) addRouteWithParentGateway(fqdnBackend bool, parentGateway string) {
	s.addHTTPRoute(parentGateway, envoyBackendRef(backendName, ""))
	s.addBackend(backendName, namespace, fqdnBackend)
}

func (s *FQDNBackendColdStartTestSuite) addPolicy(healthCheck *egv1a1.HealthCheck) {
	s.addPolicyTargeting(healthCheck, "HTTPRoute", routeName)
}

func (s *FQDNBackendColdStartTestSuite) addPolicyTargeting(healthCheck *egv1a1.HealthCheck, targetKind gatewayv1.Kind, targetName gatewayv1.ObjectName) {
	policy := s.newPolicy(healthCheck)
	policy.Spec.TargetRefs = []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		{LocalPolicyTargetReference: localTargetRef(targetKind, targetName)},
	}
	s.ctx.AddObject(policyName, policy)
}

// addPolicyWithDeprecatedTargetRef uses BackendTrafficPolicy's singular
// targetRef field rather than targetRefs. It is deprecated upstream but still
// common in manifests in the wild, so the check has to follow it too.
func (s *FQDNBackendColdStartTestSuite) addPolicyWithDeprecatedTargetRef(healthCheck *egv1a1.HealthCheck, targetKind gatewayv1.Kind, targetName gatewayv1.ObjectName) {
	policy := s.newPolicy(healthCheck)
	//nolint:staticcheck // exercising the deprecated field is the point of this helper
	policy.Spec.TargetRef = &gatewayv1.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: localTargetRef(targetKind, targetName),
	}
	s.ctx.AddObject(policyName, policy)
}

func (s *FQDNBackendColdStartTestSuite) newPolicy(healthCheck *egv1a1.HealthCheck) *egv1a1.BackendTrafficPolicy {
	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: namespace},
		Spec: egv1a1.BackendTrafficPolicySpec{
			ClusterSettings: egv1a1.ClusterSettings{HealthCheck: healthCheck},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	return policy
}

func localTargetRef(kind gatewayv1.Kind, name gatewayv1.ObjectName) gatewayv1.LocalPolicyTargetReference {
	return gatewayv1.LocalPolicyTargetReference{Group: gatewayv1.GroupName, Kind: kind, Name: name}
}

// wantMessage builds the diagnostic the check emits, given the coverage
// clause describing how the policy reaches the backend.
func wantMessage(coverage, backend string) string {
	return fmt.Sprintf(
		"BackendTrafficPolicy has no health check, but %s Backend %q with FQDN endpoint %q; "+
			"this backend resolves via DNS at startup and can 503 before the name resolves",
		coverage, backend, fqdnHost)
}

func directCoverage(kind, name string) string {
	return fmt.Sprintf("targets %s %q, which routes to", kind, name)
}

func gatewayCoverage(gateway, routeKind, route string) string {
	return fmt.Sprintf("targets Gateway %q, whose attached %s %q routes to", gateway, routeKind, route)
}

func (s *FQDNBackendColdStartTestSuite) expectDiagnostics(messages ...string) {
	diagnostics := make([]diagnostic.Diagnostic, 0, len(messages))
	for _, msg := range messages {
		diagnostics = append(diagnostics, diagnostic.Diagnostic{Message: msg})
	}
	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{policyName: diagnostics}},
	})
}

func (s *FQDNBackendColdStartTestSuite) expectNoDiagnostics() {
	s.Validate(s.ctx, []templates.TestCase{
		{Diagnostics: map[string][]diagnostic.Diagnostic{}},
	})
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsFQDNBackendWithNoHealthCheck() {
	s.addRoute(true)
	s.addPolicy(nil)

	s.expectDiagnostics(wantMessage(directCoverage("HTTPRoute", routeName), backendName))
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsGRPCRouteWithFQDNBackend() {
	s.addGRPCRoute("partner-api-grpc-route", "", envoyBackendRef(backendName, ""))
	s.addBackend(backendName, namespace, true)
	s.addPolicyTargeting(nil, "GRPCRoute", "partner-api-grpc-route")

	s.expectDiagnostics(wantMessage(directCoverage("GRPCRoute", "partner-api-grpc-route"), backendName))
}

// The singular targetRef is deprecated upstream but still appears in real
// manifests, so it has to resolve exactly like targetRefs does.
func (s *FQDNBackendColdStartTestSuite) TestFlagsViaDeprecatedSingularTargetRef() {
	s.addRoute(true)
	s.addPolicyWithDeprecatedTargetRef(nil, "HTTPRoute", routeName)

	s.expectDiagnostics(wantMessage(directCoverage("HTTPRoute", routeName), backendName))
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsCrossNamespaceBackendRef() {
	s.addHTTPRoute("", envoyBackendRef(backendName, "upstreams"))
	s.addBackend(backendName, "upstreams", true)
	s.addPolicy(nil)

	s.expectDiagnostics(wantMessage(directCoverage("HTTPRoute", routeName), backendName))
}

// Each FQDN-backed backendRef on the route is its own cold-start risk, so
// each gets its own diagnostic.
func (s *FQDNBackendColdStartTestSuite) TestFlagsEveryFQDNBackendOnTheRoute() {
	s.addHTTPRoute("",
		envoyBackendRef(backendName, ""),
		envoyBackendRef("billing-api-upstream", ""),
		serviceBackendRef("partner-api-service"),
	)
	s.addBackend(backendName, namespace, true)
	s.addBackend("billing-api-upstream", namespace, true)
	s.addPolicy(nil)

	s.expectDiagnostics(
		wantMessage(directCoverage("HTTPRoute", routeName), backendName),
		wantMessage(directCoverage("HTTPRoute", routeName), "billing-api-upstream"),
	)
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenPassiveHealthCheckConfigured() {
	s.addRoute(true)
	s.addPolicy(&egv1a1.HealthCheck{Passive: &egv1a1.PassiveHealthCheck{}})

	s.expectNoDiagnostics()
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenActiveHealthCheckConfigured() {
	s.addRoute(true)
	s.addPolicy(&egv1a1.HealthCheck{Active: &egv1a1.ActiveHealthCheck{}})

	s.expectNoDiagnostics()
}

// An empty healthCheck block sets neither active nor passive, so it closes
// nothing and must still be flagged.
func (s *FQDNBackendColdStartTestSuite) TestFlagsEmptyHealthCheckBlock() {
	s.addRoute(true)
	s.addPolicy(&egv1a1.HealthCheck{})

	s.expectDiagnostics(wantMessage(directCoverage("HTTPRoute", routeName), backendName))
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendIsNotFQDN() {
	s.addRoute(false)
	s.addPolicy(nil)

	s.expectNoDiagnostics()
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendHasNoEndpoints() {
	s.addHTTPRoute("", envoyBackendRef(backendName, ""))
	backend := &egv1a1.Backend{ObjectMeta: metav1.ObjectMeta{Name: backendName, Namespace: namespace}}
	backend.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject(backendName, backend)
	s.addPolicy(nil)

	s.expectNoDiagnostics()
}

// A backendRef naming a Backend that was never passed to gwlint cannot be
// judged, so the check stays quiet rather than guessing.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendIsNotInContext() {
	s.addHTTPRoute("", envoyBackendRef("missing-upstream", ""))
	s.addPolicy(nil)

	s.expectNoDiagnostics()
}

// Same Backend name, different namespace than the backendRef points at.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendIsInAnotherNamespace() {
	s.addHTTPRoute("", envoyBackendRef(backendName, "upstreams"))
	s.addBackend(backendName, "other", true)
	s.addPolicy(nil)

	s.expectNoDiagnostics()
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenBackendRefIsCoreService() {
	s.addHTTPRoute("", serviceBackendRef("partner-api-service"))
	s.addPolicy(nil)

	s.expectNoDiagnostics()
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenTargetRouteIsNotInContext() {
	s.addRoute(true)
	s.addPolicyTargeting(nil, "HTTPRoute", "some-other-route")

	s.expectNoDiagnostics()
}

// A targetRef naming a GRPCRoute must not resolve to a same-named HTTPRoute.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenTargetKindDoesNotMatchRouteKind() {
	s.addRoute(true)
	s.addPolicyTargeting(nil, "GRPCRoute", routeName)

	s.expectNoDiagnostics()
}

// TargetSelectors and ListenerSet targets are deliberately not resolved; an
// unrecognized target kind must not produce a false positive.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenTargetKindIsUnsupported() {
	s.addRoute(true)
	s.addPolicyTargeting(nil, "ListenerSet", "public-listeners")

	s.expectNoDiagnostics()
}

func (s *FQDNBackendColdStartTestSuite) TestFlagsGatewayLevelPolicyCoveringFQDNRoute() {
	s.addRouteWithParentGateway(true, "public-gateway")
	s.addPolicyTargeting(nil, "Gateway", "public-gateway")

	s.expectDiagnostics(wantMessage(gatewayCoverage("public-gateway", "HTTPRoute", routeName), backendName))
}

// A Gateway-level policy applies to every route attached to that Gateway, so
// each FQDN-backed route on it is flagged.
func (s *FQDNBackendColdStartTestSuite) TestFlagsEveryRouteAttachedToTargetedGateway() {
	s.addHTTPRoute("public-gateway", envoyBackendRef(backendName, ""))
	s.addBackend(backendName, namespace, true)
	s.addGRPCRoute("billing-api-route", "public-gateway", envoyBackendRef("billing-api-upstream", ""))
	s.addBackend("billing-api-upstream", namespace, true)
	s.addPolicyTargeting(nil, "Gateway", "public-gateway")

	s.expectDiagnostics(
		wantMessage(gatewayCoverage("public-gateway", "HTTPRoute", routeName), backendName),
		wantMessage(gatewayCoverage("public-gateway", "GRPCRoute", "billing-api-route"), "billing-api-upstream"),
	)
}

func (s *FQDNBackendColdStartTestSuite) TestPassesWhenRouteNotAttachedToTargetedGateway() {
	s.addRouteWithParentGateway(true, "some-other-gateway")
	s.addPolicyTargeting(nil, "Gateway", "public-gateway")

	s.expectNoDiagnostics()
}

// A parentRef whose kind is not Gateway (a ListenerSet, say) does not attach
// the route to a Gateway of that name.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenParentRefIsNotAGateway() {
	s.addHTTPRouteWithParentRefs([]gatewayv1.ParentReference{{
		Kind: kindPtr("ListenerSet"),
		Name: "public-gateway",
	}}, envoyBackendRef(backendName, ""))
	s.addBackend(backendName, namespace, true)
	s.addPolicyTargeting(nil, "Gateway", "public-gateway")

	s.expectNoDiagnostics()
}

// A Gateway in another namespace than the policy is a different Gateway, even
// with the same name.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenParentGatewayIsInAnotherNamespace() {
	s.addHTTPRouteWithParentRefs([]gatewayv1.ParentReference{{
		Namespace: nsPtr("infra"),
		Name:      "public-gateway",
	}}, envoyBackendRef(backendName, ""))
	s.addBackend(backendName, namespace, true)
	s.addPolicyTargeting(nil, "Gateway", "public-gateway")

	s.expectNoDiagnostics()
}

// A policy with no targetRef at all reaches no route, so it has nothing to
// flag even though it has no health check.
func (s *FQDNBackendColdStartTestSuite) TestPassesWhenPolicyHasNoTargetRefs() {
	s.addRoute(true)
	s.ctx.AddObject(policyName, s.newPolicy(nil))

	s.expectNoDiagnostics()
}

// A Gateway-level policy reaches routes in other namespaces that attach to it
// via parentRefs. Gateway API defaults an omitted backendRef namespace to the
// route's namespace, not the policy's, so the two below pin that down: the
// check used to look the Backend up in the policy's namespace, which silently
// found nothing, or found an unrelated object of the same name.
func (s *FQDNBackendColdStartTestSuite) addCrossNamespaceSetup() {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "ledger-route", Namespace: "ledger"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{
				ParentRefs: []gatewayv1.ParentReference{{
					Namespace: nsPtr("infra"),
					Name:      "edge-gateway",
				}},
			},
			Rules: []gatewayv1.HTTPRouteRule{{
				BackendRefs: []gatewayv1.HTTPBackendRef{{
					// No namespace: defaults to the route's, "ledger".
					BackendRef: gatewayv1.BackendRef{BackendObjectReference: envoyBackendRef("ledger-upstream", "")},
				}},
			}},
		},
	}
	route.SetGroupVersionKind(routeGVK("HTTPRoute"))
	s.ctx.AddObject("ledger-route", route)
	s.addBackend("ledger-upstream", "ledger", true)

	policy := &egv1a1.BackendTrafficPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName, Namespace: "infra"},
		Spec: egv1a1.BackendTrafficPolicySpec{
			PolicyTargetReferences: egv1a1.PolicyTargetReferences{
				TargetRefs: []gatewayv1.LocalPolicyTargetReferenceWithSectionName{
					{LocalPolicyTargetReference: localTargetRef("Gateway", "edge-gateway")},
				},
			},
		},
	}
	policy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackendTrafficPolicy))
	s.ctx.AddObject(policyName, policy)
}

func (s *FQDNBackendColdStartTestSuite) TestResolvesBackendInRouteNamespaceNotPolicyNamespace() {
	s.addCrossNamespaceSetup()

	s.expectDiagnostics(wantMessage(gatewayCoverage("edge-gateway", "HTTPRoute", "ledger-route"), "ledger-upstream"))
}

// With an unrelated Backend of the same name sitting in the policy's namespace,
// resolving against the wrong namespace does not just miss: it names the wrong
// object and reports its hostname.
func (s *FQDNBackendColdStartTestSuite) TestIgnoresSameNamedBackendInPolicyNamespace() {
	s.addCrossNamespaceSetup()
	decoy := &egv1a1.Backend{
		ObjectMeta: metav1.ObjectMeta{Name: "ledger-upstream", Namespace: "infra"},
		Spec: egv1a1.BackendSpec{Endpoints: []egv1a1.BackendEndpoint{
			{FQDN: &egv1a1.FQDNEndpoint{Hostname: "unrelated.internal", Port: 443}},
		}},
	}
	decoy.SetGroupVersionKind(egv1a1.GroupVersion.WithKind(egv1a1.KindBackend))
	s.ctx.AddObject("decoy-backend", decoy)

	s.expectDiagnostics(wantMessage(gatewayCoverage("edge-gateway", "HTTPRoute", "ledger-route"), "ledger-upstream"))
}
