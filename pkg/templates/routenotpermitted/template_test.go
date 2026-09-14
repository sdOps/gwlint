package routenotpermitted

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	routeName = "tenant-route"
	gatewayNS = "infra"
	gatewayNm = "shared-gateway"
)

func TestRouteNotPermitted(t *testing.T) { suite.Run(t, new(RouteNotPermittedTestSuite)) }

type RouteNotPermittedTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *RouteNotPermittedTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}
func fromPtr(f gatewayv1.FromNamespaces) *gatewayv1.FromNamespaces { return &f }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace             { return &n }

func (s *RouteNotPermittedTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayNm, Namespace: gatewayNS},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway", gw)
}

func listener(name gatewayv1.SectionName, protocol gatewayv1.ProtocolType, allowed *gatewayv1.AllowedRoutes) gatewayv1.Listener {
	return gatewayv1.Listener{Name: name, Port: 443, Protocol: protocol, AllowedRoutes: allowed}
}

func (s *RouteNotPermittedTestSuite) addRouteIn(ns string) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: ns},
		Spec: gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{
			ParentRefs: []gatewayv1.ParentReference{{Name: gatewayNm, Namespace: nsPtr(gatewayv1.Namespace(gatewayNS))}},
		}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *RouteNotPermittedTestSuite) expectContains(fragments ...string) {
	s.T().Helper()
	if len(fragments) == 0 {
		s.Validate(s.ctx, []templates.TestCase{{Diagnostics: map[string][]diagnostic.Diagnostic{}}})
		return
	}
	found := checkAll(s.ctx)
	s.Require().Len(found, 1)
	for _, f := range fragments {
		s.Contains(found[0], f)
	}
}

// checkAll runs the check over every object, since the assertions here are on
// message fragments rather than exact strings.
func checkAll(ctx *mocks.MockLintContext) []string {
	var messages []string
	for _, obj := range ctx.Objects() {
		for _, d := range checkFunc(ctx, obj) {
			messages = append(messages, d.Message)
		}
	}
	return messages
}

// The default is Same, so a Gateway in another namespace does not admit the route.
func (s *RouteNotPermittedTestSuite) TestFlagsNamespaceRefusedByDefault() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, nil))
	s.addRouteIn("tenant-a")

	s.expectContains(`admits routes from "Same" namespaces and the route is in "tenant-a"`)
}

func (s *RouteNotPermittedTestSuite) TestPassesWhenTheRouteSharesTheGatewayNamespace() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, nil))
	s.addRouteIn(gatewayNS)

	s.expectContains()
}

func (s *RouteNotPermittedTestSuite) TestPassesWhenAllNamespacesAreAdmitted() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, &gatewayv1.AllowedRoutes{
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromAll)},
	}))
	s.addRouteIn("tenant-a")

	s.expectContains()
}

// A label Selector needs Namespace objects a rendered set rarely carries, so it
// is treated as permitting rather than guessed at.
func (s *RouteNotPermittedTestSuite) TestTreatsANamespaceSelectorAsPermitting() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, &gatewayv1.AllowedRoutes{
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromSelector)},
	}))
	s.addRouteIn("tenant-a")

	s.expectContains()
}

func (s *RouteNotPermittedTestSuite) TestFlagsAKindTheListenerDoesNotAccept() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, &gatewayv1.AllowedRoutes{
		Kinds:      []gatewayv1.RouteGroupKind{{Kind: "GRPCRoute"}},
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromAll)},
	}))
	s.addRouteIn("tenant-a")

	s.expectContains("accepts GRPCRoute and the route is a HTTPRoute")
}

func (s *RouteNotPermittedTestSuite) TestPassesWhenTheKindIsAccepted() {
	s.addGateway(listener("https", gatewayv1.HTTPSProtocolType, &gatewayv1.AllowedRoutes{
		Kinds:      []gatewayv1.RouteGroupKind{{Kind: "HTTPRoute"}, {Kind: "GRPCRoute"}},
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromAll)},
	}))
	s.addRouteIn("tenant-a")

	s.expectContains()
}

// A route attaches if ANY listener admits it, so one permissive listener is
// enough and the route must not be flagged.
func (s *RouteNotPermittedTestSuite) TestOneAdmittingListenerIsEnough() {
	s.addGateway(
		listener("strict", gatewayv1.HTTPSProtocolType, nil),
		listener("open", gatewayv1.HTTPSProtocolType, &gatewayv1.AllowedRoutes{
			Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromAll)},
		}),
	)
	s.addRouteIn("tenant-a")

	s.expectContains()
}

// With no Gateway in the set there is nothing to judge against.
func (s *RouteNotPermittedTestSuite) TestStaysQuietWhenTheParentIsAbsent() {
	s.addRouteIn("tenant-a")

	s.expectContains()
}

// A kind refused only by the listener's protocol default, with no explicit
// allowedRoutes.kinds narrowing it, is protocol-mismatch's finding. Reporting
// it here too would say the same thing twice.
func (s *RouteNotPermittedTestSuite) TestDoesNotDuplicateAProtocolDefaultKindMismatch() {
	s.addGateway(listener("raw-tcp", gatewayv1.TCPProtocolType, nil))
	s.addRouteIn(gatewayNS)

	s.expectContains()
}
