package protocolmismatch

import (
	"fmt"
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
	gatewayName = "pmm-gateway"
	routeName   = "pmm-route"
	namespace   = "pmm"
)

func TestProtocolMismatch(t *testing.T) {
	suite.Run(t, new(ProtocolMismatchTestSuite))
}

type ProtocolMismatchTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *ProtocolMismatchTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind { return &k }
func sectionPtr(n gatewayv1.SectionName) *gatewayv1.SectionName {
	return &n
}
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func fromPtr(f gatewayv1.FromNamespaces) *gatewayv1.FromNamespaces {
	return &f
}

func listener(name string, protocol gatewayv1.ProtocolType, port int32) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: protocol,
	}
}

func (s *ProtocolMismatchTestSuite) addGateway(name, ns string, listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg", Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-"+ns+"-"+name, gw)
}

func (s *ProtocolMismatchTestSuite) addRoute(ns string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: ns},
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *ProtocolMismatchTestSuite) addGRPCRoute(name string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GRPCRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("GRPCRoute"))
	s.ctx.AddObject(name, route)
}

func (s *ProtocolMismatchTestSuite) addTLSRoute(name string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.TLSRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("TLSRoute"))
	s.ctx.AddObject(name, route)
}

func (s *ProtocolMismatchTestSuite) addUDPRoute(name string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.UDPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.UDPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("UDPRoute"))
	s.ctx.AddObject(name, route)
}

func want(routeKind, route, parent, reason string) string {
	return fmt.Sprintf(
		"%s %q attaches to Gateway %q, whose protocol cannot carry it: %s; "+
			"the route is never programmed and its traffic is not served",
		routeKind, route, parent, reason)
}

func (s *ProtocolMismatchTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *ProtocolMismatchTestSuite) TestFlagsHTTPRouteOnATCPListener() {
	s.addGateway(gatewayName, namespace, listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, gatewayName,
			`listener "tcp" speaks TCP, which carries TCPRoute`)},
	})
}

func (s *ProtocolMismatchTestSuite) TestPassesWhenTheProtocolCarriesTheRouteKind() {
	s.addGateway(gatewayName, namespace, listener("http", gatewayv1.HTTPProtocolType, 80))
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// HTTPS carries gRPC as well as HTTP.
func (s *ProtocolMismatchTestSuite) TestHTTPSCarriesGRPCRoutes() {
	s.addGateway(gatewayName, namespace, listener("https", gatewayv1.HTTPSProtocolType, 443))
	s.addGRPCRoute("pmm-grpc-route", gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *ProtocolMismatchTestSuite) TestFlagsTLSRouteOnAnHTTPListener() {
	s.addGateway(gatewayName, namespace, listener("http", gatewayv1.HTTPProtocolType, 80))
	s.addTLSRoute("pmm-tls-route", gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		"pmm-tls-route": {want("TLSRoute", "pmm-tls-route", gatewayName,
			`listener "http" speaks HTTP, which carries HTTPRoute and GRPCRoute`)},
	})
}

func (s *ProtocolMismatchTestSuite) TestFlagsUDPRouteOnATCPListener() {
	s.addGateway(gatewayName, namespace, listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addUDPRoute("pmm-udp-route", gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		"pmm-udp-route": {want("UDPRoute", "pmm-udp-route", gatewayName,
			`listener "tcp" speaks TCP, which carries TCPRoute`)},
	})
}

// An allowedRoutes.kinds naming a kind the protocol cannot carry advertises
// something the listener can never serve. Nothing else reports this one: the
// listener admits the route by kind and then cannot carry it.
func (s *ProtocolMismatchTestSuite) TestFlagsAllowedKindsTheProtocolCannotCarry() {
	l := listener("tcp", gatewayv1.TCPProtocolType, 9000)
	l.AllowedRoutes = &gatewayv1.AllowedRoutes{Kinds: []gatewayv1.RouteGroupKind{{Kind: "HTTPRoute"}}}
	s.addGateway(gatewayName, namespace, l)
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, gatewayName,
			`listener "tcp" allows HTTPRoute but speaks TCP, which carries TCPRoute`)},
	})
}

// When the listener's own allowedRoutes.kinds already excludes the route,
// route-not-permitted reports it and this check stays out of the way.
func (s *ProtocolMismatchTestSuite) TestStaysQuietWhenAllowedKindsAlreadyExcludesTheRoute() {
	l := listener("tcp", gatewayv1.TCPProtocolType, 9000)
	l.AllowedRoutes = &gatewayv1.AllowedRoutes{Kinds: []gatewayv1.RouteGroupKind{{Kind: "TLSRoute"}}}
	s.addGateway(gatewayName, namespace, l)
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// Without a sectionName the route attaches to every listener, so one that can
// carry it is enough.
func (s *ProtocolMismatchTestSuite) TestPassesWhenAnyListenerCarriesTheRoute() {
	s.addGateway(gatewayName, namespace,
		listener("tcp", gatewayv1.TCPProtocolType, 9000),
		listener("http", gatewayv1.HTTPProtocolType, 80),
	)
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *ProtocolMismatchTestSuite) TestFlagsWhenEverySelectedListenerRefuses() {
	s.addGateway(gatewayName, namespace,
		listener("tcp", gatewayv1.TCPProtocolType, 9000),
		listener("udp", gatewayv1.UDPProtocolType, 9001),
	)
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, gatewayName,
			`listener "tcp" speaks TCP, which carries TCPRoute; `+
				`listener "udp" speaks UDP, which carries UDPRoute`)},
	})
}

// A sectionName narrows the attachment to one listener, so a sibling that
// could have carried the route no longer saves it.
func (s *ProtocolMismatchTestSuite) TestSectionNameNarrowsToOneListener() {
	s.addGateway(gatewayName, namespace,
		listener("http", gatewayv1.HTTPProtocolType, 80),
		listener("tcp", gatewayv1.TCPProtocolType, 9000),
	)
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName, SectionName: sectionPtr("tcp")})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, gatewayName,
			`listener "tcp" speaks TCP, which carries TCPRoute`)},
	})
}

// Linting routes without the chart that defines the Gateway is normal: with no
// listener to compare against, the check stays quiet rather than flagging every
// route in the set.
func (s *ProtocolMismatchTestSuite) TestStaysQuietWhenTheParentIsAbsent() {
	s.addRoute(namespace, gatewayv1.ParentReference{Name: "gateway-defined-elsewhere"})

	s.expect(nil)
}

// A Gateway of the same name in another namespace is not this route's parent.
func (s *ProtocolMismatchTestSuite) TestStaysQuietWhenOnlyAnotherNamespaceHasTheGateway() {
	s.addGateway(gatewayName, "other", listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// An unresolved sectionName is listener-not-found's finding.
func (s *ProtocolMismatchTestSuite) TestStaysQuietWhenTheSectionNameResolvesToNothing() {
	s.addGateway(gatewayName, namespace, listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName, SectionName: sectionPtr("tpc")})

	s.expect(nil)
}

// A parentRef into another implementation's API group is not ours to resolve.
func (s *ProtocolMismatchTestSuite) TestIgnoresParentRefsInOtherAPIGroups() {
	s.addGateway(gatewayName, namespace, listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addRoute(namespace, gatewayv1.ParentReference{
		Group: grpPtr("some.vendor.io"),
		Kind:  kindPtr("MeshService"),
		Name:  "not-a-gateway",
	})

	s.expect(nil)
}

// A listener that refuses the route's namespace refuses it for a reason that
// has nothing to do with protocol; route-not-permitted says so.
func (s *ProtocolMismatchTestSuite) TestSkipsListenersThatRefuseTheRouteNamespace() {
	l := listener("tcp", gatewayv1.TCPProtocolType, 9000)
	l.AllowedRoutes = &gatewayv1.AllowedRoutes{
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromSame)},
	}
	s.addGateway(gatewayName, namespace, l)
	s.addRoute("tenant", gatewayv1.ParentReference{Name: gatewayName, Namespace: nsPtr(namespace)})

	s.expect(nil)
}

// An implementation-specific protocol carries whatever its implementation says
// it does, which gwlint has no way to know.
func (s *ProtocolMismatchTestSuite) TestIgnoresImplementationSpecificProtocols() {
	s.addGateway(gatewayName, namespace, listener("custom", "example.com/proxy", 7000))
	s.addRoute(namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *ProtocolMismatchTestSuite) TestReportsEachParentSeparately() {
	s.addGateway(gatewayName, namespace, listener("tcp", gatewayv1.TCPProtocolType, 9000))
	s.addGateway("pmm-other-gateway", namespace, listener("udp", gatewayv1.UDPProtocolType, 9001))
	s.addRoute(namespace,
		gatewayv1.ParentReference{Name: gatewayName},
		gatewayv1.ParentReference{Name: "pmm-other-gateway"},
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, gatewayName,
				`listener "tcp" speaks TCP, which carries TCPRoute`),
			want("HTTPRoute", routeName, "pmm-other-gateway",
				`listener "udp" speaks UDP, which carries UDPRoute`),
		},
	})
}
