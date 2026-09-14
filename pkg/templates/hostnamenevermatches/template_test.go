package hostnamenevermatches

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
	gatewayName = "hnm-gateway"
	routeName   = "hnm-route"
	namespace   = "hnm"
)

func TestHostnameNeverMatches(t *testing.T) {
	suite.Run(t, new(HostnameNeverMatchesTestSuite))
}

type HostnameNeverMatchesTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *HostnameNeverMatchesTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func hostPtr(h gatewayv1.Hostname) *gatewayv1.Hostname { return &h }
func sectionPtr(n gatewayv1.SectionName) *gatewayv1.SectionName {
	return &n
}
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }
func fromPtr(f gatewayv1.FromNamespaces) *gatewayv1.FromNamespaces {
	return &f
}

// listener builds an HTTPS listener, which is the shape hostnames matter on.
func listener(name string, hostname *gatewayv1.Hostname) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     443,
		Protocol: gatewayv1.HTTPSProtocolType,
		Hostname: hostname,
	}
}

func (s *HostnameNeverMatchesTestSuite) addGateway(name, ns string, listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg", Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-"+ns+"-"+name, gw)
}

func (s *HostnameNeverMatchesTestSuite) addRoute(ns string, hostnames []gatewayv1.Hostname, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: ns},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
			Hostnames:       hostnames,
		},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *HostnameNeverMatchesTestSuite) addTLSRoute(name string, hostnames []gatewayv1.Hostname, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gatewayv1.TLSRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs},
			Hostnames:       hostnames,
		},
	}
	route.SetGroupVersionKind(gvk("TLSRoute"))
	s.ctx.AddObject(name, route)
}

func (s *HostnameNeverMatchesTestSuite) addTCPRoute(name string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.TCPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("TCPRoute"))
	s.ctx.AddObject(name, route)
}

func want(routeKind, route, hosts, parent, offered string) string {
	return fmt.Sprintf(
		"%s %q asks for %s on Gateway %q, where %s; the hostnames cannot intersect, "+
			"so no request can ever match the route",
		routeKind, route, hosts, parent, offered)
}

func (s *HostnameNeverMatchesTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *HostnameNeverMatchesTestSuite) TestFlagsRouteHostnameOutsideTheListenerWildcard() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.internal.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, `hostname "shop.example.com"`,
			gatewayName, `listener "web" serves "*.internal.example.com"`)},
	})
}

func (s *HostnameNeverMatchesTestSuite) TestPassesWhenTheWildcardCoversTheRouteHostname() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *HostnameNeverMatchesTestSuite) TestFlagsTwoExactHostnamesThatDiffer() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("api.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"web.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, `hostname "web.example.com"`,
			gatewayName, `listener "web" serves "api.example.com"`)},
	})
}

func (s *HostnameNeverMatchesTestSuite) TestPassesWhenTheExactHostnamesAgree() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("api.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"api.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// A listener with no hostname serves every name, so nothing can fail to
// intersect it.
func (s *HostnameNeverMatchesTestSuite) TestPassesWhenTheListenerSetsNoHostname() {
	s.addGateway(gatewayName, namespace, listener("web", nil))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// A route with no hostnames takes whatever the listener offers.
func (s *HostnameNeverMatchesTestSuite) TestPassesWhenTheRouteSetsNoHostname() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("api.example.com")))
	s.addRoute(namespace, nil, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// Without a sectionName the route attaches to every listener, so one that can
// match it is enough.
func (s *HostnameNeverMatchesTestSuite) TestPassesWhenAnyListenerMatches() {
	s.addGateway(gatewayName, namespace,
		listener("internal", hostPtr("*.internal.example.com")),
		listener("public", hostPtr("*.example.com")),
	)
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *HostnameNeverMatchesTestSuite) TestFlagsWhenEverySelectedListenerMisses() {
	s.addGateway(gatewayName, namespace,
		listener("internal", hostPtr("*.internal.example.com")),
		listener("partner", hostPtr("*.partner.example.net")),
	)
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, `hostname "shop.example.com"`, gatewayName,
			`listener "internal" serves "*.internal.example.com" and listener "partner" serves "*.partner.example.net"`)},
	})
}

// A sectionName narrows the attachment to one listener, so a sibling that
// would have matched no longer saves the route.
func (s *HostnameNeverMatchesTestSuite) TestSectionNameNarrowsToOneListener() {
	s.addGateway(gatewayName, namespace,
		listener("public", hostPtr("*.example.com")),
		listener("internal", hostPtr("*.internal.example.net")),
	)
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName, SectionName: sectionPtr("internal")})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, `hostname "shop.example.com"`,
			gatewayName, `listener "internal" serves "*.internal.example.net"`)},
	})
}

// Only one of the route's hostnames has to reach the listener.
func (s *HostnameNeverMatchesTestSuite) TestPassesWhenOneOfSeveralRouteHostnamesMatches() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.net", "shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *HostnameNeverMatchesTestSuite) TestListsEveryRouteHostnameWhenNoneMatch() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.net", "api.example.org"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, `hostnames "shop.example.net", "api.example.org"`,
			gatewayName, `listener "web" serves "*.example.com"`)},
	})
}

// Linting routes without the chart that defines the Gateway is normal: with no
// listener to compare against, the check has nothing to judge and stays quiet
// rather than flagging every route in the set.
func (s *HostnameNeverMatchesTestSuite) TestStaysQuietWhenTheParentIsAbsent() {
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: "gateway-defined-elsewhere"})

	s.expect(nil)
}

// A Gateway of the same name in another namespace is not this route's parent,
// so its listeners say nothing about this route.
func (s *HostnameNeverMatchesTestSuite) TestStaysQuietWhenOnlyAnotherNamespaceHasTheGateway() {
	s.addGateway(gatewayName, "other", listener("web", hostPtr("*.internal.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// An unresolved sectionName is listener-not-found's finding.
func (s *HostnameNeverMatchesTestSuite) TestStaysQuietWhenTheSectionNameResolvesToNothing() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.internal.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName, SectionName: sectionPtr("wbe")})

	s.expect(nil)
}

// A parentRef into another implementation's API group is not ours to resolve.
func (s *HostnameNeverMatchesTestSuite) TestIgnoresParentRefsInOtherAPIGroups() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.internal.example.com")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"}, gatewayv1.ParentReference{
		Group: grpPtr("some.vendor.io"),
		Kind:  func() *gatewayv1.Kind { k := gatewayv1.Kind("MeshService"); return &k }(),
		Name:  "not-a-gateway",
	})

	s.expect(nil)
}

// A listener that will not admit the route's namespace never attaches it for a
// reason that has nothing to do with hostnames; route-not-permitted says so.
func (s *HostnameNeverMatchesTestSuite) TestSkipsListenersThatRefuseTheRouteNamespace() {
	l := listener("web", hostPtr("*.internal.example.com"))
	l.AllowedRoutes = &gatewayv1.AllowedRoutes{
		Namespaces: &gatewayv1.RouteNamespaces{From: fromPtr(gatewayv1.NamespacesFromSame)},
	}
	s.addGateway(gatewayName, namespace, l)
	s.addRoute("tenant", []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName, Namespace: nsPtr(namespace)})

	s.expect(nil)
}

// Likewise for a listener whose allowedRoutes.kinds excludes the route.
func (s *HostnameNeverMatchesTestSuite) TestSkipsListenersThatRefuseTheRouteKind() {
	l := listener("web", hostPtr("*.internal.example.com"))
	l.AllowedRoutes = &gatewayv1.AllowedRoutes{
		Kinds: []gatewayv1.RouteGroupKind{{Kind: "GRPCRoute"}},
	}
	s.addGateway(gatewayName, namespace, l)
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// TLSRoute carries hostnames too, and a TLS listener has one to compare.
func (s *HostnameNeverMatchesTestSuite) TestAppliesToTLSRoutes() {
	tls := gatewayv1.Listener{
		Name: "tls", Port: 443, Protocol: gatewayv1.TLSProtocolType,
		Hostname: hostPtr("*.internal.example.com"),
	}
	s.addGateway(gatewayName, namespace, tls)
	s.addTLSRoute("hnm-tls-route", []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{
		"hnm-tls-route": {want("TLSRoute", "hnm-tls-route", `hostname "shop.example.com"`,
			gatewayName, `listener "tls" serves "*.internal.example.com"`)},
	})
}

// TCPRoute has no hostnames at all, so there is nothing to intersect.
func (s *HostnameNeverMatchesTestSuite) TestIgnoresRouteKindsWithoutHostnames() {
	tcp := gatewayv1.Listener{Name: "tcp", Port: 9000, Protocol: gatewayv1.TCPProtocolType}
	s.addGateway(gatewayName, namespace, tcp)
	s.addTCPRoute("hnm-tcp-route", gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *HostnameNeverMatchesTestSuite) TestReportsEachParentSeparately() {
	s.addGateway(gatewayName, namespace, listener("web", hostPtr("*.internal.example.com")))
	s.addGateway("hnm-other-gateway", namespace, listener("edge", hostPtr("*.partner.example.net")))
	s.addRoute(namespace, []gatewayv1.Hostname{"shop.example.com"},
		gatewayv1.ParentReference{Name: gatewayName},
		gatewayv1.ParentReference{Name: "hnm-other-gateway"},
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, `hostname "shop.example.com"`,
				gatewayName, `listener "web" serves "*.internal.example.com"`),
			want("HTTPRoute", routeName, `hostname "shop.example.com"`,
				"hnm-other-gateway", `listener "edge" serves "*.partner.example.net"`),
		},
	})
}
