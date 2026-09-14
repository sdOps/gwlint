package danglinggatewayclass

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
	gatewayName = "reporting-gateway"
	namespace   = "reporting"
)

func TestDanglingGatewayClass(t *testing.T) {
	suite.Run(t, new(DanglingGatewayClassTestSuite))
}

type DanglingGatewayClassTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DanglingGatewayClassTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(version, kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: version}.WithKind(kind)
}

func (s *DanglingGatewayClassTestSuite) addGatewayClass() {
	class := &gatewayv1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: "eg"},
		Spec:       gatewayv1.GatewayClassSpec{ControllerName: "gateway.envoyproxy.io/gatewayclass-controller"},
	}
	class.SetGroupVersionKind(gvk("v1", gwobjectkinds.GatewayClass))
	s.ctx.AddObject("gatewayclass-eg", class)
}

func (s *DanglingGatewayClassTestSuite) addBetaGatewayClass(name string) {
	class := &gatewayv1b1.GatewayClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       gatewayv1.GatewayClassSpec{ControllerName: "gateway.envoyproxy.io/gatewayclass-controller"},
	}
	class.SetGroupVersionKind(gvk("v1beta1", gwobjectkinds.GatewayClass))
	s.ctx.AddObject("gatewayclass-beta-"+name, class)
}

func (s *DanglingGatewayClassTestSuite) addGateway(name, className string) {
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(className)},
	}
	gateway.SetGroupVersionKind(gvk("v1", gwobjectkinds.Gateway))
	s.ctx.AddObject(name, gateway)
}

func (s *DanglingGatewayClassTestSuite) addBetaGateway(name, className string) {
	gateway := &gatewayv1b1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: gatewayv1.ObjectName(className)},
	}
	gateway.SetGroupVersionKind(gvk("v1beta1", gwobjectkinds.Gateway))
	s.ctx.AddObject(name, gateway)
}

func want(gateway, className string) string {
	return fmt.Sprintf(
		"Gateway %q names GatewayClass %q, which is not present; "+
			"no controller will claim the Gateway and none of its listeners will be programmed",
		gateway, className)
}

func (s *DanglingGatewayClassTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DanglingGatewayClassTestSuite) TestFlagsAGatewayClassNameThatIsNotPresent() {
	s.addGatewayClass()
	s.addGateway(gatewayName, "envoy-gateway")

	s.expect(map[string][]string{
		gatewayName: {want(gatewayName, "envoy-gateway")},
	})
}

func (s *DanglingGatewayClassTestSuite) TestPassesWhenTheGatewayClassExists() {
	s.addGatewayClass()
	s.addGateway(gatewayName, "eg")

	s.expect(nil)
}

// GatewayClass is cluster scoped, so the Gateway's namespace has no bearing on
// whether the class resolves.
func (s *DanglingGatewayClassTestSuite) TestGatewayClassIsClusterScoped() {
	s.addGatewayClass()
	gateway := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "elsewhere-gateway", Namespace: "some-other-namespace"},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg"},
	}
	gateway.SetGroupVersionKind(gvk("v1", gwobjectkinds.Gateway))
	s.ctx.AddObject("elsewhere-gateway", gateway)

	s.expect(nil)
}

// A GatewayClass ships with the controller, in a different chart from the
// Gateways that name it, so a set with no GatewayClass at all is not evidence
// that anything is missing.
func (s *DanglingGatewayClassTestSuite) TestStaysQuietWhenNoGatewayClassWasPassedAtAll() {
	s.addGateway(gatewayName, "eg")

	s.expect(nil)
}

// An absent gatewayClassName is a schema violation; saying so here would only
// duplicate what kubeconform reports better.
func (s *DanglingGatewayClassTestSuite) TestIgnoresAnEmptyGatewayClassName() {
	s.addGatewayClass()
	s.addGateway(gatewayName, "")

	s.expect(nil)
}

// v1beta1 is still served, and its Gateway is a distinct Go type rather than
// an alias, so both directions of the version mix have to resolve.
func (s *DanglingGatewayClassTestSuite) TestJudgesABetaGateway() {
	s.addGatewayClass()
	s.addBetaGateway("beta-gateway", "not-installed")

	s.expect(map[string][]string{
		"beta-gateway": {want("beta-gateway", "not-installed")},
	})
}

func (s *DanglingGatewayClassTestSuite) TestABetaGatewayClassSatisfiesAV1Gateway() {
	s.addBetaGatewayClass("eg")
	s.addGateway(gatewayName, "eg")

	s.expect(nil)
}

func (s *DanglingGatewayClassTestSuite) TestFlagsEveryGatewayNamingAMissingClass() {
	s.addGatewayClass()
	s.addGateway(gatewayName, "eg-typo")
	s.addGateway("billing-gateway", "eg")
	s.addGateway("search-gateway", "never-installed")

	s.expect(map[string][]string{
		gatewayName:      {want(gatewayName, "eg-typo")},
		"search-gateway": {want("search-gateway", "never-installed")},
	})
}
