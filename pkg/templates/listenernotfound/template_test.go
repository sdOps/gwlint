package listenernotfound

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
	routeName = "edge-route"
	namespace = "edge"
	gatewayNm = "edge-gateway"
)

func TestListenerNotFound(t *testing.T) { suite.Run(t, new(ListenerNotFoundTestSuite)) }

type ListenerNotFoundTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *ListenerNotFoundTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}
func sectionPtr(n gatewayv1.SectionName) *gatewayv1.SectionName { return &n }
func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind                  { return &k }

func (s *ListenerNotFoundTestSuite) addGateway(names ...gatewayv1.SectionName) {
	listeners := make([]gatewayv1.Listener, 0, len(names))
	for _, n := range names {
		listeners = append(listeners, gatewayv1.Listener{Name: n, Port: 443, Protocol: gatewayv1.HTTPSProtocolType})
	}
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayNm, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway", gw)
}

func (s *ListenerNotFoundTestSuite) addListenerSet(name string, listeners ...gatewayv1.SectionName) {
	entries := make([]gatewayv1.ListenerEntry, 0, len(listeners))
	for _, n := range listeners {
		entries = append(entries, gatewayv1.ListenerEntry{Name: n, Port: 8443, Protocol: gatewayv1.HTTPSProtocolType})
	}
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.ListenerSetSpec{Listeners: entries},
	}
	ls.SetGroupVersionKind(gvk("ListenerSet"))
	s.ctx.AddObject("listenerset", ls)
}

func (s *ListenerNotFoundTestSuite) addRoute(refs ...gatewayv1.ParentReference) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: routeName, Namespace: namespace},
		Spec:       gatewayv1.HTTPRouteSpec{CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: refs}},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(routeName, route)
}

func want(section, parentKind, parent, has string) string {
	return fmt.Sprintf(
		"HTTPRoute %q attaches to listener %q on %s %q, which declares no such listener (it has %s); "+
			"the route is not accepted onto that parent and its traffic is not served",
		routeName, section, parentKind, parent, has)
}

func (s *ListenerNotFoundTestSuite) expect(messages ...string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for _, m := range messages {
		expected[routeName] = append(expected[routeName], diagnostic.Diagnostic{Message: m})
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *ListenerNotFoundTestSuite) TestFlagsAMisspeltListenerName() {
	s.addGateway("http", "https", "sftp")
	s.addRoute(gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("htps")})

	s.expect(want("htps", "Gateway", gatewayNm, `"http", "https" and "sftp"`))
}

func (s *ListenerNotFoundTestSuite) TestPassesWhenTheListenerExists() {
	s.addGateway("http", "https")
	s.addRoute(gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("https")})

	s.expect()
}

// No sectionName attaches the route to every listener, so there is nothing to
// resolve and nothing to report.
func (s *ListenerNotFoundTestSuite) TestPassesWhenNoListenerIsNamed() {
	s.addGateway("http", "https")
	s.addRoute(gatewayv1.ParentReference{Name: gatewayNm})

	s.expect()
}

// A parent that is not in the manifest set is dangling-parent-ref's finding.
// Reporting it here too would say the same thing twice.
func (s *ListenerNotFoundTestSuite) TestStaysQuietWhenTheParentIsAbsent() {
	s.addRoute(gatewayv1.ParentReference{Name: "gateway-elsewhere", SectionName: sectionPtr("https")})

	s.expect()
}

func (s *ListenerNotFoundTestSuite) TestResolvesListenerSetListeners() {
	s.addGateway("https")
	s.addListenerSet("extra-listeners", "internal")
	s.addRoute(gatewayv1.ParentReference{
		Kind: kindPtr("ListenerSet"), Name: "extra-listeners", SectionName: sectionPtr("internal"),
	})

	s.expect()
}

func (s *ListenerNotFoundTestSuite) TestFlagsAMissingListenerSetListener() {
	s.addGateway("https")
	s.addListenerSet("extra-listeners", "internal")
	s.addRoute(gatewayv1.ParentReference{
		Kind: kindPtr("ListenerSet"), Name: "extra-listeners", SectionName: sectionPtr("external"),
	})

	s.expect(want("external", "ListenerSet", "extra-listeners", `only "internal"`))
}

func (s *ListenerNotFoundTestSuite) TestNamesASingleListenerReadably() {
	s.addGateway("https")
	s.addRoute(gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("http")})

	s.expect(want("http", "Gateway", gatewayNm, `only "https"`))
}

func (s *ListenerNotFoundTestSuite) TestReportsEveryBadSectionNameOnARoute() {
	s.addGateway("https")
	s.addRoute(
		gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("https")},
		gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("one")},
		gatewayv1.ParentReference{Name: gatewayNm, SectionName: sectionPtr("two")},
	)

	s.expect(
		want("one", "Gateway", gatewayNm, `only "https"`),
		want("two", "Gateway", gatewayNm, `only "https"`),
	)
}
