package missingreferencegrant

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
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

const (
	routeName = "orders-route"
	routeNS   = "storefront"
	backendNS = "payments"
	backend   = "orders-api"
)

func TestMissingReferenceGrant(t *testing.T) {
	suite.Run(t, new(MissingReferenceGrantTestSuite))
}

type MissingReferenceGrantTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *MissingReferenceGrantTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind             { return &k }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace     { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group            { return &g }
func namePtr(n gatewayv1.ObjectName) *gatewayv1.ObjectName { return &n }

// backendRef builds a backendRef pointing into another namespace, which is the
// only shape this check has an opinion about.
func backendRef(name, namespace string) gatewayv1.HTTPBackendRef {
	return gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{
			Name:      gatewayv1.ObjectName(name),
			Namespace: nsPtr(gatewayv1.Namespace(namespace)),
		},
	}}
}

func (s *MissingReferenceGrantTestSuite) addRoute(namespace string, refs ...gatewayv1.HTTPBackendRef) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{Rules: []gatewayv1.HTTPRouteRule{{BackendRefs: refs}}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func (s *MissingReferenceGrantTestSuite) addGrant(name, namespace string, spec gatewayv1.ReferenceGrantSpec) {
	grant := &gatewayv1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
	grant.SetGroupVersionKind(gvk("ReferenceGrant"))
	s.ctx.AddObject("grant-"+namespace+"-"+name, grant)
}

// allowServiceFromHTTPRoute is the grant a correctly configured chart ships:
// every Service in the namespace, reachable by HTTPRoutes from routeNS.
func allowServiceFromHTTPRoute() gatewayv1.ReferenceGrantSpec {
	return gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: routeNS},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: "", Kind: "Service"}},
	}
}

func want(routeKind, route, toKind, toName string) string {
	return fmt.Sprintf(
		"%s %q in namespace %q routes to %s %q in namespace %q, but no ReferenceGrant there "+
			"permits %s from namespace %q to reference it; the backendRef resolves to nothing "+
			"and requests matching that rule fail",
		routeKind, route, routeNS, toKind, toName, backendNS, routeKind, routeNS)
}

func (s *MissingReferenceGrantTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *MissingReferenceGrantTestSuite) TestFlagsCrossNamespaceRefWithNoMatchingGrant() {
	// A grant exists in the target namespace, but for a different consumer.
	s.addGrant("allow-other", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "somewhere-else"},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: "", Kind: "Service"}},
	})
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

func (s *MissingReferenceGrantTestSuite) TestPassesWhenAGrantPermitsTheReference() {
	s.addGrant("allow-storefront", backendNS, allowServiceFromHTTPRoute())
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

// A grant naming the backend explicitly is as good as one naming the kind.
func (s *MissingReferenceGrantTestSuite) TestPassesWhenTheGrantNamesTheBackend() {
	spec := allowServiceFromHTTPRoute()
	spec.To[0].Name = namePtr(backend)
	s.addGrant("allow-orders-api", backendNS, spec)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

func (s *MissingReferenceGrantTestSuite) TestFlagsWhenTheGrantNamesADifferentBackend() {
	spec := allowServiceFromHTTPRoute()
	spec.To[0].Name = namePtr("refunds-api")
	s.addGrant("allow-refunds-api", backendNS, spec)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

// A grant for certificateRefs does not cover backendRefs.
func (s *MissingReferenceGrantTestSuite) TestToKindMustMatch() {
	spec := allowServiceFromHTTPRoute()
	spec.To[0].Kind = "Secret"
	s.addGrant("allow-secrets", backendNS, spec)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

// A grant for core Services does not cover a ref into a vendor's API group,
// even where the kind reads the same.
func (s *MissingReferenceGrantTestSuite) TestToGroupMustMatch() {
	s.addGrant("allow-storefront-services", backendNS, allowServiceFromHTTPRoute())
	ref := backendRef("orders-upstream", backendNS)
	ref.Group = grpPtr(egv1a1.GroupName)
	ref.Kind = kindPtr(egv1a1.KindBackend)
	s.addRoute(routeNS, ref)

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, egv1a1.KindBackend, "orders-upstream")},
	})
}

func (s *MissingReferenceGrantTestSuite) TestPassesWhenTheGrantCoversTheBackendKind() {
	s.addGrant("allow-storefront-backends", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: routeNS},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: egv1a1.GroupName, Kind: egv1a1.KindBackend}},
	})
	ref := backendRef("orders-upstream", backendNS)
	ref.Group = grpPtr(egv1a1.GroupName)
	ref.Kind = kindPtr(egv1a1.KindBackend)
	s.addRoute(routeNS, ref)

	s.expect(nil)
}

// The route kind on the from side is matched exactly, so a grant written for
// HTTPRoutes leaves a GRPCRoute in the same namespace denied.
func (s *MissingReferenceGrantTestSuite) TestFromKindMustMatch() {
	s.addGrant("allow-storefront-http", backendNS, allowServiceFromHTTPRoute())
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "grpc-orders-route", Namespace: routeNS},
		Spec: gatewayv1.GRPCRouteSpec{Rules: []gatewayv1.GRPCRouteRule{{
			BackendRefs: []gatewayv1.GRPCBackendRef{{BackendRef: gatewayv1.BackendRef{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: backend, Namespace: nsPtr(backendNS),
				},
			}}},
		}}},
	}
	route.SetGroupVersionKind(gvk("GRPCRoute"))
	s.ctx.AddObject("grpc-orders-route", route)

	s.expect(map[string][]string{
		"grpc-orders-route": {want("GRPCRoute", "grpc-orders-route", "Service", backend)},
	})
}

func (s *MissingReferenceGrantTestSuite) TestFromNamespaceMustMatch() {
	spec := allowServiceFromHTTPRoute()
	spec.From[0].Namespace = "warehouse"
	s.addGrant("allow-warehouse", backendNS, spec)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

// An empty from.group means core, so it never covers a Gateway API route.
func (s *MissingReferenceGrantTestSuite) TestFromGroupMustBeGatewayAPI() {
	spec := allowServiceFromHTTPRoute()
	spec.From[0].Group = ""
	s.addGrant("allow-coreish", backendNS, spec)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

// from and to are both OR lists, so one matching entry in each is enough.
func (s *MissingReferenceGrantTestSuite) TestMatchesAnyEntryOnEitherSide() {
	s.addGrant("allow-several", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "warehouse"},
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: routeNS},
		},
		To: []gatewayv1.ReferenceGrantTo{
			{Group: "", Kind: "Secret"},
			{Group: "", Kind: "Service"},
		},
	})
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

// A namespace can carry several grants, each adding to what is permitted.
func (s *MissingReferenceGrantTestSuite) TestPassesWhenASecondGrantPermitsIt() {
	s.addGrant("allow-warehouse-only", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: "warehouse"},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: "", Kind: "Service"}},
	})
	s.addGrant("allow-storefront-too", backendNS, allowServiceFromHTTPRoute())
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

// A grant only ever authorises references into its own namespace, so one
// sitting in the route's namespace grants nothing.
func (s *MissingReferenceGrantTestSuite) TestGrantMustLiveInTheTargetNamespace() {
	s.addGrant("misplaced-grant", routeNS, allowServiceFromHTTPRoute())
	s.addGrant("unrelated-grant", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "TCPRoute", Namespace: "warehouse"},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: "", Kind: "Service"}},
	})
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(map[string][]string{
		routeName: {want("HTTPRoute", routeName, "Service", backend)},
	})
}

// A backendRef with no namespace stays in the route's own namespace, where no
// grant is needed.
func (s *MissingReferenceGrantTestSuite) TestIgnoresSameNamespaceRefs() {
	s.addGrant("allow-storefront", backendNS, allowServiceFromHTTPRoute())
	local := gatewayv1.HTTPBackendRef{BackendRef: gatewayv1.BackendRef{
		BackendObjectReference: gatewayv1.BackendObjectReference{Name: "local-api"},
	}}
	s.addRoute(routeNS, local)

	s.expect(nil)
}

// Spelling out the route's own namespace is not a cross-namespace reference.
func (s *MissingReferenceGrantTestSuite) TestIgnoresRefsNamingTheRouteNamespace() {
	s.addGrant("allow-storefront", backendNS, allowServiceFromHTTPRoute())
	s.addRoute(routeNS, backendRef("local-api", routeNS))

	s.expect(nil)
}

// Rendered charts routinely leave metadata.namespace off and let helm supply
// it, and without it there is no telling whether the reference crosses a
// boundary at all.
func (s *MissingReferenceGrantTestSuite) TestStaysQuietWhenTheRouteHasNoNamespace() {
	s.addGrant("allow-storefront", backendNS, allowServiceFromHTTPRoute())
	s.addRoute("", backendRef(backend, backendNS))

	s.expect(nil)
}

// Grants ship with the workloads they protect, so a set with none for the
// target namespace has not been told about that namespace's grants.
func (s *MissingReferenceGrantTestSuite) TestStaysQuietWhenNoGrantsWerePassedAtAll() {
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

func (s *MissingReferenceGrantTestSuite) TestStaysQuietWhenNoGrantCoversTheTargetNamespace() {
	s.addGrant("allow-storefront-elsewhere", "warehouse", allowServiceFromHTTPRoute())
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

func (s *MissingReferenceGrantTestSuite) TestReportsEveryUnpermittedBackendRef() {
	s.addGrant("allow-orders-api-only", backendNS, gatewayv1.ReferenceGrantSpec{
		From: []gatewayv1.ReferenceGrantFrom{
			{Group: gatewayv1.GroupName, Kind: "HTTPRoute", Namespace: routeNS},
		},
		To: []gatewayv1.ReferenceGrantTo{{Group: "", Kind: "Service", Name: namePtr(backend)}},
	})
	s.addRoute(routeNS,
		backendRef(backend, backendNS),
		backendRef("refunds-api", backendNS),
		backendRef("ledger-api", backendNS),
	)

	s.expect(map[string][]string{
		routeName: {
			want("HTTPRoute", routeName, "Service", "refunds-api"),
			want("HTTPRoute", routeName, "Service", "ledger-api"),
		},
	})
}

// ReferenceGrant is most often authored as v1beta1 and still served as
// v1alpha2, and a grant has to count whichever version it was written in.
func (s *MissingReferenceGrantTestSuite) TestRecognisesV1Beta1Grants() {
	grant := &gatewayv1b1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-storefront-v1beta1", Namespace: backendNS},
		Spec:       allowServiceFromHTTPRoute(),
	}
	grant.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: "v1beta1",
	}.WithKind("ReferenceGrant"))
	s.ctx.AddObject("allow-storefront-v1beta1", grant)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

func (s *MissingReferenceGrantTestSuite) TestRecognisesV1Alpha2Grants() {
	grant := &gatewayv1a2.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "allow-storefront-v1alpha2", Namespace: backendNS},
		Spec:       allowServiceFromHTTPRoute(),
	}
	grant.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: "v1alpha2",
	}.WithKind("ReferenceGrant"))
	s.ctx.AddObject("allow-storefront-v1alpha2", grant)
	s.addRoute(routeNS, backendRef(backend, backendNS))

	s.expect(nil)
}

// Every route kind carries backendRefs and every one of them needs a grant,
// including the v1alpha2 stream routes.
func (s *MissingReferenceGrantTestSuite) TestAppliesToStreamRoutes() {
	s.addGrant("allow-storefront-http", backendNS, allowServiceFromHTTPRoute())
	route := &gatewayv1a2.TCPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "orders-tcp-route", Namespace: routeNS},
		Spec: gatewayv1a2.TCPRouteSpec{Rules: []gatewayv1a2.TCPRouteRule{{
			BackendRefs: []gatewayv1.BackendRef{{
				BackendObjectReference: gatewayv1.BackendObjectReference{
					Name: backend, Namespace: nsPtr(backendNS),
				},
			}},
		}}},
	}
	route.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: "v1alpha2",
	}.WithKind("TCPRoute"))
	s.ctx.AddObject("orders-tcp-route", route)

	s.expect(map[string][]string{
		"orders-tcp-route": {want("TCPRoute", "orders-tcp-route", "Service", backend)},
	})
}

// Nothing but a route has backendRefs to judge.
func (s *MissingReferenceGrantTestSuite) TestIgnoresNonRouteObjects() {
	s.addGrant("allow-storefront", backendNS, allowServiceFromHTTPRoute())
	gw := &gatewayv1.Gateway{ObjectMeta: metav1.ObjectMeta{Name: "edge-gateway", Namespace: routeNS}}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("edge-gateway", gw)

	s.expect(nil)
}
