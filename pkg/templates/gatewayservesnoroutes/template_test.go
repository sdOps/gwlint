package gatewayservesnoroutes

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
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	gatewayName = "edge-gateway"
	namespace   = "edge"
)

func TestGatewayServesNoRoutes(t *testing.T) {
	suite.Run(t, new(GatewayServesNoRoutesTestSuite))
}

type GatewayServesNoRoutesTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *GatewayServesNoRoutesTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(group, version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: group, Version: version}.WithKind(kind)
}

func v1gvk(kind string) schema.GroupVersionKind {
	return gvk(gatewayv1.GroupName, gatewayv1.GroupVersion.Version, kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }

func (s *GatewayServesNoRoutesTestSuite) addGateway(name string) {
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	gw.SetGroupVersionKind(v1gvk("Gateway"))
	s.ctx.AddObject("gateway-"+namespace+"-"+name, gw)
}

// v1beta1's Gateway is a separate Go type over the same struct, and charts
// still emit it, so the check has to see it too.
func (s *GatewayServesNoRoutesTestSuite) addV1Beta1Gateway(name, ns string) {
	gw := &gatewayv1b1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	gw.SetGroupVersionKind(gvk(gatewayv1b1.GroupName, gatewayv1b1.GroupVersion.Version, "Gateway"))
	s.ctx.AddObject("gateway-"+ns+"-"+name, gw)
}

func (s *GatewayServesNoRoutesTestSuite) addListenerSet(name, ns string, parent gatewayv1.ParentGatewayReference) {
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.ListenerSetSpec{ParentRef: parent},
	}
	ls.SetGroupVersionKind(v1gvk("ListenerSet"))
	s.ctx.AddObject("listenerset-"+ns+"-"+name, ls)
}

func (s *GatewayServesNoRoutesTestSuite) addRoute(name, ns string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(v1gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

// TCPRoute is usually authored as v1alpha2 and attaches exactly the same way.
func (s *GatewayServesNoRoutesTestSuite) addTCPRoute(name, ns string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1a2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1a2.TCPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk(gatewayv1a2.GroupName, gatewayv1a2.GroupVersion.Version, "TCPRoute"))
	s.ctx.AddObject(name, route)
}

func want() string {
	return fmt.Sprintf(
		"Gateway %q in namespace %q has no route attached; no route in the manifest set names it "+
			"as a parentRef, directly or through a ListenerSet, so it is provisioned and serves no traffic",
		gatewayName, namespace)
}

func (s *GatewayServesNoRoutesTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// The route in the set attaches somewhere else, so this Gateway serves nothing
// while the one it does name is fine.
func (s *GatewayServesNoRoutesTestSuite) TestFlagsAGatewayNoRouteNames() {
	s.addGateway(gatewayName)
	s.addGateway("busy-gateway")
	s.addRoute("storefront-route", namespace, gatewayv1.ParentReference{Name: "busy-gateway"})

	s.expect(map[string][]string{gatewayName: {want()}})
}

func (s *GatewayServesNoRoutesTestSuite) TestPassesWhenARouteAttaches() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// Linting a platform repo that holds Gateways and no routes is normal, so with
// no route in the set at all the check stays quiet rather than flagging every
// Gateway it was handed.
func (s *GatewayServesNoRoutesTestSuite) TestStaysQuietWhenNoRoutesWerePassedAtAll() {
	s.addGateway(gatewayName)
	s.addGateway("busy-gateway")

	s.expect(nil)
}

// A route anywhere in the set lifts the guard, including one in another
// namespace, since routes attach across namespaces all the time.
func (s *GatewayServesNoRoutesTestSuite) TestARouteInAnotherNamespaceStillLiftsTheGuard() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", "storefront", gatewayv1.ParentReference{Name: "storefront-gateway"})

	s.expect(map[string][]string{gatewayName: {want()}})
}

func (s *GatewayServesNoRoutesTestSuite) TestPassesWhenARouteAttachesFromAnotherNamespace() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", "storefront",
		gatewayv1.ParentReference{Name: gatewayName, Namespace: nsPtr(namespace)})

	s.expect(nil)
}

// A parentRef with no namespace means the route's own, so a route elsewhere
// naming the same Gateway name does not reach this Gateway.
func (s *GatewayServesNoRoutesTestSuite) TestParentRefDefaultsToTheRouteNamespace() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", "storefront", gatewayv1.ParentReference{Name: gatewayName})

	s.expect(map[string][]string{gatewayName: {want()}})
}

// The attachment hierarchy is Gateway to ListenerSet to Route, so a route that
// names a ListenerSet is serving the Gateway that ListenerSet belongs to.
func (s *GatewayServesNoRoutesTestSuite) TestPassesWhenARouteAttachesThroughAListenerSet() {
	s.addGateway(gatewayName)
	s.addListenerSet("extra-listeners", namespace, gatewayv1.ParentGatewayReference{Name: gatewayName})
	s.addRoute("storefront-route", namespace,
		gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "extra-listeners"})

	s.expect(nil)
}

func (s *GatewayServesNoRoutesTestSuite) TestPassesWhenTheListenerSetAttachesAcrossNamespaces() {
	s.addGateway(gatewayName)
	s.addListenerSet("remote-listeners", "storefront",
		gatewayv1.ParentGatewayReference{Name: gatewayName, Namespace: nsPtr(namespace)})
	s.addRoute("storefront-route", "storefront",
		gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "remote-listeners"})

	s.expect(nil)
}

// The ListenerSet belongs to another Gateway, so routes attaching to it say
// nothing about this one.
func (s *GatewayServesNoRoutesTestSuite) TestFlagsWhenTheListenerSetBelongsToAnotherGateway() {
	s.addGateway(gatewayName)
	s.addGateway("busy-gateway")
	s.addListenerSet("extra-listeners", namespace, gatewayv1.ParentGatewayReference{Name: "busy-gateway"})
	s.addRoute("storefront-route", namespace,
		gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "extra-listeners"})

	s.expect(map[string][]string{gatewayName: {want()}})
}

// A parentRef asking for a ListenerSet does not attach to a Gateway that
// happens to share the name.
func (s *GatewayServesNoRoutesTestSuite) TestKindMustMatch() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", namespace,
		gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: gatewayName})

	s.expect(map[string][]string{gatewayName: {want()}})
}

// A parentRef into another vendor's API group is that implementation's
// attachment point, not this Gateway, even when the name lines up.
func (s *GatewayServesNoRoutesTestSuite) TestIgnoresParentRefsInOtherAPIGroups() {
	s.addGateway(gatewayName)
	s.addRoute("storefront-route", namespace, gatewayv1.ParentReference{
		Group: grpPtr("some.vendor.io"),
		Kind:  kindPtr("Gateway"),
		Name:  gatewayName,
	})

	s.expect(map[string][]string{gatewayName: {want()}})
}

// Every route kind attaches the same way, and the stream kinds are usually
// written as v1alpha2.
func (s *GatewayServesNoRoutesTestSuite) TestAnyRouteKindCounts() {
	s.addGateway(gatewayName)
	s.addTCPRoute("database-route", namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

func (s *GatewayServesNoRoutesTestSuite) TestJudgesAV1Beta1Gateway() {
	s.addV1Beta1Gateway(gatewayName, namespace)
	s.addRoute("storefront-route", namespace, gatewayv1.ParentReference{Name: "busy-gateway"})

	s.expect(map[string][]string{gatewayName: {want()}})
}

func (s *GatewayServesNoRoutesTestSuite) TestPassesWhenARouteAttachesToAV1Beta1Gateway() {
	s.addV1Beta1Gateway(gatewayName, namespace)
	s.addRoute("storefront-route", namespace, gatewayv1.ParentReference{Name: gatewayName})

	s.expect(nil)
}

// A route with no parentRefs at all attaches to nothing, but its presence is
// still evidence that routes were passed in.
func (s *GatewayServesNoRoutesTestSuite) TestARouteWithNoParentRefsAttachesToNothing() {
	s.addGateway(gatewayName)
	s.addRoute("orphan-route", namespace)

	s.expect(map[string][]string{gatewayName: {want()}})
}

// One attaching route is enough, however many others point elsewhere.
func (s *GatewayServesNoRoutesTestSuite) TestOneAttachingRouteAmongManyIsEnough() {
	s.addGateway(gatewayName)
	s.addRoute("first-route", namespace, gatewayv1.ParentReference{Name: "somewhere-else"})
	s.addRoute("second-route", namespace,
		gatewayv1.ParentReference{Name: "somewhere-else"},
		gatewayv1.ParentReference{Name: gatewayName},
	)

	s.expect(nil)
}
