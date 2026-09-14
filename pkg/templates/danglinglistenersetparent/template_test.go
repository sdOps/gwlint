package danglinglistenersetparent

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
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const (
	listenerSetName = "extra-listeners"
	namespace       = "edge"
)

func TestDanglingListenerSetParent(t *testing.T) {
	suite.Run(t, new(DanglingListenerSetParentTestSuite))
}

type DanglingListenerSetParentTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DanglingListenerSetParentTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: version}.WithKind(kind)
}

func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind         { return &k }
func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace { return &n }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group        { return &g }

func (s *DanglingListenerSetParentTestSuite) addGateway(name, ns string) {
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	gateway.SetGroupVersionKind(gvk("v1", gwobjectkinds.Gateway))
	s.ctx.AddObject("gateway-"+ns+"-"+name, gateway)
}

func (s *DanglingListenerSetParentTestSuite) addBetaGateway(name, ns string) {
	gateway := &gatewayv1b1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	gateway.SetGroupVersionKind(gvk("v1beta1", gwobjectkinds.Gateway))
	s.ctx.AddObject("gateway-beta-"+ns+"-"+name, gateway)
}

func (s *DanglingListenerSetParentTestSuite) addListenerSet(name string, parentRef gatewayv1.ParentGatewayReference) {
	listenerSet := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gatewayv1.ListenerSetSpec{
			ParentRef: parentRef,
			Listeners: []gatewayv1.ListenerEntry{{
				Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
			}},
		},
	}
	listenerSet.SetGroupVersionKind(gvk("v1", gwobjectkinds.ListenerSet))
	s.ctx.AddObject(name, listenerSet)
}

func want(listenerSet, gateway, ns string) string {
	return fmt.Sprintf(
		"ListenerSet %q attaches to Gateway %q in namespace %q, which is not present; "+
			"its listeners will never be merged into a Gateway and will never be bound",
		listenerSet, gateway, ns)
}

func (s *DanglingListenerSetParentTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DanglingListenerSetParentTestSuite) TestFlagsAParentRefNamingAMissingGateway() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "rael-gateway"})

	s.expect(map[string][]string{
		listenerSetName: {want(listenerSetName, "rael-gateway", namespace)},
	})
}

func (s *DanglingListenerSetParentTestSuite) TestPassesWhenTheGatewayExists() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "real-gateway"})

	s.expect(nil)
}

// An omitted namespace means the ListenerSet's own, so a Gateway of the same
// name elsewhere does not satisfy the ref.
func (s *DanglingListenerSetParentTestSuite) TestParentRefDefaultsToTheListenerSetNamespace() {
	s.addGateway("shared-gateway", "infra")
	s.addGateway("local-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "shared-gateway"})

	s.expect(map[string][]string{
		listenerSetName: {want(listenerSetName, "shared-gateway", namespace)},
	})
}

func (s *DanglingListenerSetParentTestSuite) TestPassesWhenTheParentRefNamesTheOtherNamespace() {
	s.addGateway("shared-gateway", "infra")
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Name: "shared-gateway", Namespace: nsPtr("infra"),
	})

	s.expect(nil)
}

// The Gateway a ListenerSet extends usually comes from another chart, so a set
// carrying no Gateway at all is not evidence that the parent is missing.
func (s *DanglingListenerSetParentTestSuite) TestStaysQuietWhenNoGatewayWasPassedAtAll() {
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "gateway-defined-elsewhere"})

	s.expect(nil)
}

// The guard is per namespace: Gateways for one namespace say nothing about
// what exists in another the ref reaches into.
func (s *DanglingListenerSetParentTestSuite) TestStaysQuietWhenTheTargetNamespaceIsNotDescribed() {
	s.addGateway("local-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Name: "infra-gateway", Namespace: nsPtr("infra"),
	})

	s.expect(nil)
}

func (s *DanglingListenerSetParentTestSuite) TestFlagsACrossNamespaceParentRefOnceThatNamespaceIsDescribed() {
	s.addGateway("infra-gateway", "infra")
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Name: "infra-gatway", Namespace: nsPtr("infra"),
	})

	s.expect(map[string][]string{
		listenerSetName: {want(listenerSetName, "infra-gatway", "infra")},
	})
}

// v1beta1 Gateways are a distinct Go type from v1's, and a parentRef does not
// care which version the chart emitted.
func (s *DanglingListenerSetParentTestSuite) TestABetaGatewaySatisfiesTheParentRef() {
	s.addBetaGateway("beta-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "beta-gateway"})

	s.expect(nil)
}

// Spelling out the defaults changes nothing.
func (s *DanglingListenerSetParentTestSuite) TestExplicitGroupAndKindAreStillJudged() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Group: grpPtr(gatewayv1.GroupName), Kind: kindPtr("Gateway"), Name: "rael-gateway",
	})

	s.expect(map[string][]string{
		listenerSetName: {want(listenerSetName, "rael-gateway", namespace)},
	})
}

func (s *DanglingListenerSetParentTestSuite) TestIgnoresAParentRefInAnotherAPIGroup() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Group: grpPtr("some.vendor.io"), Kind: kindPtr("Gateway"), Name: "vendor-gateway",
	})

	s.expect(nil)
}

func (s *DanglingListenerSetParentTestSuite) TestIgnoresAParentRefAtAnotherKind() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{
		Kind: kindPtr("MeshGateway"), Name: "not-a-gateway",
	})

	s.expect(nil)
}

// A parentRef with no name is a schema violation, not a dangling reference.
func (s *DanglingListenerSetParentTestSuite) TestIgnoresAnEmptyName() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{})

	s.expect(nil)
}

func (s *DanglingListenerSetParentTestSuite) TestJudgesEveryListenerSetInTheSet() {
	s.addGateway("real-gateway", namespace)
	s.addListenerSet(listenerSetName, gatewayv1.ParentGatewayReference{Name: "real-gateway"})
	s.addListenerSet("tcp-listeners", gatewayv1.ParentGatewayReference{Name: "retired-gateway"})

	s.expect(map[string][]string{
		"tcp-listeners": {want("tcp-listeners", "retired-gateway", namespace)},
	})
}
