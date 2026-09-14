package duplicatelistenername

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
	gatewayName = "edge-gateway"
	namespace   = "infra"
)

func TestDuplicateListenerName(t *testing.T) {
	suite.Run(t, new(DuplicateListenerNameTestSuite))
}

type DuplicateListenerNameTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *DuplicateListenerNameTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

// listener is the minimum a listener needs to be addressable: a name, a port
// and a protocol.
func listener(name string, port int32) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: gatewayv1.HTTPProtocolType,
	}
}

func (s *DuplicateListenerNameTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject(gatewayName, gw)
}

func (s *DuplicateListenerNameTestSuite) addListenerSet(name string, entries ...gatewayv1.ListenerEntry) {
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.ListenerSetSpec{Listeners: entries},
	}
	ls.SetGroupVersionKind(gvk("ListenerSet"))
	s.ctx.AddObject(name, ls)
}

func entry(name string, port int32) gatewayv1.ListenerEntry {
	return gatewayv1.ListenerEntry{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: gatewayv1.HTTPProtocolType,
	}
}

// A route that is not a Gateway at all must leave the check silent, since the
// test suite runs the check function over every object in the context.
func (s *DuplicateListenerNameTestSuite) addRoute(name string) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

func want(ownerKind, owner string, count int, name string) string {
	return fmt.Sprintf(
		"%s %q declares %d listeners named %q; a listener name must be unique within a %s, "+
			"so the API server rejects the object and a route sectionName naming that listener "+
			"has no single listener to attach to",
		ownerKind, owner, count, name, ownerKind)
}

func (s *DuplicateListenerNameTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func (s *DuplicateListenerNameTestSuite) TestFlagsTwoListenersSharingAName() {
	s.addGateway(listener("web", 80), listener("web", 8080))

	s.expect(map[string][]string{
		gatewayName: {want("Gateway", gatewayName, 2, "web")},
	})
}

func (s *DuplicateListenerNameTestSuite) TestPassesWhenEveryNameIsUnique() {
	s.addGateway(listener("web", 80), listener("admin", 8080))

	s.expect(nil)
}

// One finding per duplicated name, not one per extra listener, and the count
// tells the reader how many there are.
func (s *DuplicateListenerNameTestSuite) TestReportsTheNameOnceWhateverTheCount() {
	s.addGateway(listener("web", 80), listener("web", 8080), listener("web", 8081))

	s.expect(map[string][]string{
		gatewayName: {want("Gateway", gatewayName, 3, "web")},
	})
}

func (s *DuplicateListenerNameTestSuite) TestReportsEveryDuplicatedName() {
	s.addGateway(
		listener("web", 80), listener("web", 8080),
		listener("admin", 9090), listener("admin", 9091),
		listener("metrics", 9100),
	)

	s.expect(map[string][]string{
		gatewayName: {
			want("Gateway", gatewayName, 2, "web"),
			want("Gateway", gatewayName, 2, "admin"),
		},
	})
}

func (s *DuplicateListenerNameTestSuite) TestPassesOnASingleListener() {
	s.addGateway(listener("web", 80))

	s.expect(nil)
}

func (s *DuplicateListenerNameTestSuite) TestPassesOnAGatewayWithNoListeners() {
	s.addGateway()

	s.expect(nil)
}

// A ListenerSet carries the same uniqueness rule, and its listeners are
// addressed by name the same way a Gateway's are.
func (s *DuplicateListenerNameTestSuite) TestFlagsDuplicatesInAListenerSet() {
	s.addListenerSet("extra-listeners", entry("tenant", 80), entry("tenant", 8080))

	s.expect(map[string][]string{
		"extra-listeners": {want("ListenerSet", "extra-listeners", 2, "tenant")},
	})
}

// Names have to collide only within one object. A Gateway and a ListenerSet
// may each declare a listener called "web", and upstream says so explicitly.
func (s *DuplicateListenerNameTestSuite) TestNamesNeedNotBeUniqueAcrossObjects() {
	s.addGateway(listener("web", 80))
	s.addListenerSet("tenant-listeners", entry("web", 8080))

	s.expect(nil)
}

func (s *DuplicateListenerNameTestSuite) TestIgnoresObjectsThatDeclareNoListeners() {
	s.addRoute("some-route")

	s.expect(nil)
}

// The check reads one object's own spec, so a manifest set holding nothing but
// the Gateway still produces the finding. Nothing here depends on another
// object being present, which is why this check needs no partial-set guard.
func (s *DuplicateListenerNameTestSuite) TestJudgesAGatewayOnItsOwn() {
	s.addGateway(listener("web", 80), listener("web", 8080))

	s.Len(s.ctx.Objects(), 1)
	s.expect(map[string][]string{
		gatewayName: {want("Gateway", gatewayName, 2, "web")},
	})
}
