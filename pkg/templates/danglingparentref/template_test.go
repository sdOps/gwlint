package danglingparentref

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
	routeName = "partner-api-route"
	namespace = "default"
)

func TestDanglingParentRef(t *testing.T) {
	suite.Run(t, new(DanglingParentRefTestSuite))
}

type DanglingParentRefTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DanglingParentRefTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }

func (s *DanglingParentRefTestSuite) addGateway(name, ns string) {
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-"+ns+"-"+name, gw)
}

func (s *DanglingParentRefTestSuite) addListenerSet(name, ns string) {
	ls := &gatewayv1.ListenerSet{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	ls.SetGroupVersionKind(gvk("ListenerSet"))
	s.ctx.AddObject("listenerset-"+ns+"-"+name, ls)
}

func (s *DanglingParentRefTestSuite) addRoute(parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *DanglingParentRefTestSuite) addGRPCRoute(name string, parentRefs ...gatewayv1.ParentReference) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GRPCRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parentRefs}},
	}
	route.SetGroupVersionKind(gvk("GRPCRoute"))
	s.ctx.AddObject(name, route)
}

func want(routeKind, route, parentKind, parent string) string {
	return fmt.Sprintf(
		"%s %q attaches to %s %q in namespace %q, which is not present; "+
			"the route will never be programmed and its traffic will not be served",
		routeKind, route, parentKind, parent, namespace)
}

func (s *DanglingParentRefTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DanglingParentRefTestSuite) TestFlagsParentRefNamingAMissingGateway() {
	s.addGateway("real-gateway", namespace)
	s.addRoute(gatewayv1.ParentReference{Name: "typo-gateway"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Gateway", "typo-gateway")},
	})
}

func (s *DanglingParentRefTestSuite) TestPassesWhenTheGatewayExists() {
	s.addGateway("real-gateway", namespace)
	s.addRoute(gatewayv1.ParentReference{Name: "real-gateway"})

	s.expect(nil)
}

// A parentRef defaults to the route's own namespace, so a Gateway of the same
// name elsewhere does not satisfy it.
func (s *DanglingParentRefTestSuite) TestParentRefDefaultsToTheRouteNamespace() {
	s.addGateway("shared-gateway", "infra")
	s.addRoute(gatewayv1.ParentReference{Name: "shared-gateway"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Gateway", "shared-gateway")},
	})
}

func (s *DanglingParentRefTestSuite) TestPassesWhenTheParentRefNamesTheOtherNamespace() {
	s.addGateway("shared-gateway", "infra")
	s.addRoute(gatewayv1.ParentReference{Name: "shared-gateway", Namespace: nsPtr("infra")})

	s.expect(nil)
}

func (s *DanglingParentRefTestSuite) TestResolvesListenerSetParents() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet("extra-listeners", namespace)
	s.addRoute(gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "extra-listeners"})

	s.expect(nil)
}

func (s *DanglingParentRefTestSuite) TestFlagsAMissingListenerSet() {
	s.addGateway("real-gateway", namespace)
	s.addRoute(gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "absent-listeners"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "ListenerSet", "absent-listeners")},
	})
}

// A Gateway does not satisfy a parentRef that asked for a ListenerSet.
func (s *DanglingParentRefTestSuite) TestKindMustMatch() {
	s.addGateway("shared-name", namespace)
	s.addRoute(gatewayv1.ParentReference{Kind: kindPtr("ListenerSet"), Name: "shared-name"})

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "ListenerSet", "shared-name")},
	})
}

// Linting routes without the chart that defines the Gateway is normal, so with
// no Gateway or ListenerSet present at all the check stays quiet rather than
// flagging every route in the set.
func (s *DanglingParentRefTestSuite) TestStaysQuietWhenNoParentsWerePassedAtAll() {
	s.addRoute(gatewayv1.ParentReference{Name: "gateway-defined-elsewhere"})

	s.expect(nil)
}

// A parentRef into another implementation's API group is not ours to resolve.
func (s *DanglingParentRefTestSuite) TestIgnoresParentRefsInOtherAPIGroups() {
	s.addGateway("real-gateway", namespace)
	s.addRoute(gatewayv1.ParentReference{
		Group: grpPtr("some.vendor.io"),
		Kind:  kindPtr("MeshService"),
		Name:  "not-a-gateway",
	})

	s.expect(nil)
}

func (s *DanglingParentRefTestSuite) TestReportsEveryDanglingParentRefOnARoute() {
	s.addGateway("real-gateway", namespace)
	s.addRoute(
		gatewayv1.ParentReference{Name: "real-gateway"},
		gatewayv1.ParentReference{Name: "missing-one"},
		gatewayv1.ParentReference{Name: "missing-two"},
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, "Gateway", "missing-one"),
			want("HTTPRoute", routeName, "Gateway", "missing-two"),
		},
	})
}

// Every route kind attaches the same way, so all of them are judged.
func (s *DanglingParentRefTestSuite) TestAppliesToOtherRouteKinds() {
	s.addGateway("real-gateway", namespace)
	s.addGRPCRoute("grpc-route", gatewayv1.ParentReference{Name: "typo-gateway"})

	s.expect(map[string][]string{
		"grpc-route": {want("GRPCRoute", "grpc-route", "Gateway", "typo-gateway")},
	})
}
