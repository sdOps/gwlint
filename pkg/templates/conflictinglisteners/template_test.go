package conflictinglisteners

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
	gatewayName = "shared-port-gateway"
	namespace   = "infra"
)

func TestConflictingListeners(t *testing.T) {
	suite.Run(t, new(ConflictingListenersTestSuite))
}

type ConflictingListenersTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *ConflictingListenersTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func hostPtr(h string) *gatewayv1.Hostname {
	hostname := gatewayv1.Hostname(h)
	return &hostname
}

func listener(name string, port int32, protocol gatewayv1.ProtocolType, hostname *gatewayv1.Hostname) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: protocol,
		Hostname: hostname,
	}
}

func entry(name string, port int32, protocol gatewayv1.ProtocolType, hostname *gatewayv1.Hostname) gatewayv1.ListenerEntry {
	return gatewayv1.ListenerEntry{
		Name:     gatewayv1.SectionName(name),
		Port:     port,
		Protocol: protocol,
		Hostname: hostname,
	}
}

func (s *ConflictingListenersTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject(gatewayName, gw)
}

func (s *ConflictingListenersTestSuite) addListenerSet(name string, entries ...gatewayv1.ListenerEntry) {
	ls := &gatewayv1.ListenerSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.ListenerSetSpec{Listeners: entries},
	}
	ls.SetGroupVersionKind(gvk("ListenerSet"))
	s.ctx.AddObject(name, ls)
}

func (s *ConflictingListenersTestSuite) addRoute(name string) {
	route := &gatewayv1.HTTPRoute{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

const (
	rawTCPReason = "a listener sharing a port with a raw TCP listener cannot be told apart from it, " +
		"so neither of them is accepted and the port serves nothing"
	plaintextReason = "one port cannot carry both plaintext and TLS traffic, " +
		"so neither listener is accepted and the port serves nothing"
)

func wantProtocolConflict(first string, firstProtocol gatewayv1.ProtocolType,
	second string, secondProtocol gatewayv1.ProtocolType, port int32, reason string) string {
	return fmt.Sprintf(
		"Gateway %q has %s with protocol %s and %s with protocol %s on port %d; %s",
		gatewayName, first, firstProtocol, second, secondProtocol, port, reason)
}

func wantIndistinct(ownerKind, owner, listeners string,
	protocol gatewayv1.ProtocolType, hostname string) string {
	return fmt.Sprintf(
		"%s %q has %s on port %d, each with protocol %s and %s; port, protocol and hostname together "+
			"must identify one listener, so none of them is distinct and none is accepted",
		ownerKind, owner, listeners, 80, protocol, hostname)
}

func wantIndistinctWithoutHostname(ownerKind, owner, listeners string, port int32,
	protocol gatewayv1.ProtocolType) string {
	return fmt.Sprintf(
		"%s %q has %s on port %d, each with protocol %s, which carries no hostname to tell them apart; "+
			"none of them is distinct and none is accepted",
		ownerKind, owner, listeners, port, protocol)
}

func (s *ConflictingListenersTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// A raw TCP listener swallows the port: nothing sharing it can be told apart.
func (s *ConflictingListenersTestSuite) TestFlagsRawTCPSharingAPortWithHTTP() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("raw", 80, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantProtocolConflict(
			`listener "web"`, gatewayv1.HTTPProtocolType,
			`listener "raw"`, gatewayv1.TCPProtocolType, 80, rawTCPReason)},
	})
}

// The protocol rule holds whatever the hostnames say, so a distinguishing
// hostname does not rescue the pair.
func (s *ConflictingListenersTestSuite) TestProtocolConflictIgnoresHostnames() {
	s.addGateway(
		listener("web", 8443, gatewayv1.HTTPSProtocolType, hostPtr("shop.example.com")),
		listener("raw", 8443, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantProtocolConflict(
			`listener "web"`, gatewayv1.HTTPSProtocolType,
			`listener "raw"`, gatewayv1.TCPProtocolType, 8443, rawTCPReason)},
	})
}

func (s *ConflictingListenersTestSuite) TestFlagsPlaintextSharingAPortWithTLS() {
	s.addGateway(
		listener("plain", 443, gatewayv1.HTTPProtocolType, nil),
		listener("secure", 443, gatewayv1.HTTPSProtocolType, hostPtr("shop.example.com")),
	)

	s.expect(map[string][]string{
		gatewayName: {wantProtocolConflict(
			`listener "plain"`, gatewayv1.HTTPProtocolType,
			`listener "secure"`, gatewayv1.HTTPSProtocolType, 443, plaintextReason)},
	})
}

// Both sides of a pair are named, so several listeners of one protocol are
// reported together rather than once each.
func (s *ConflictingListenersTestSuite) TestNamesEveryListenerOnBothSidesOfTheConflict() {
	s.addGateway(
		listener("shop", 80, gatewayv1.HTTPProtocolType, hostPtr("shop.example.com")),
		listener("admin", 80, gatewayv1.HTTPProtocolType, hostPtr("admin.example.com")),
		listener("raw", 80, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantProtocolConflict(
			`listeners "admin" and "shop"`, gatewayv1.HTTPProtocolType,
			`listener "raw"`, gatewayv1.TCPProtocolType, 80, rawTCPReason)},
	})
}

func (s *ConflictingListenersTestSuite) TestFlagsIdenticalPortProtocolAndHostname() {
	s.addGateway(
		listener("shop", 80, gatewayv1.HTTPProtocolType, hostPtr("shop.example.com")),
		listener("shop-again", 80, gatewayv1.HTTPProtocolType, hostPtr("shop.example.com")),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinct("Gateway", gatewayName,
			`listeners "shop" and "shop-again"`, gatewayv1.HTTPProtocolType,
			`hostname "shop.example.com"`)},
	})
}

// An unset hostname is the catch-all, so two of them on a port are as
// indistinct as two identical hostnames.
func (s *ConflictingListenersTestSuite) TestFlagsTwoCatchAllListenersOnAPort() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-copy", 80, gatewayv1.HTTPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinct("Gateway", gatewayName,
			`listeners "web" and "web-copy"`, gatewayv1.HTTPProtocolType,
			"no hostname, so they match every request")},
	})
}

// An empty hostname string and an absent one mean the same thing.
func (s *ConflictingListenersTestSuite) TestTreatsAnEmptyHostnameAsUnset() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-copy", 80, gatewayv1.HTTPProtocolType, hostPtr("")),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinct("Gateway", gatewayName,
			`listeners "web" and "web-copy"`, gatewayv1.HTTPProtocolType,
			"no hostname, so they match every request")},
	})
}

func (s *ConflictingListenersTestSuite) TestPassesWhenHostnamesDistinguishTheListeners() {
	s.addGateway(
		listener("shop", 80, gatewayv1.HTTPProtocolType, hostPtr("shop.example.com")),
		listener("admin", 80, gatewayv1.HTTPProtocolType, hostPtr("admin.example.com")),
	)

	s.expect(nil)
}

// One catch-all listener alongside a hostname-bearing one is the documented
// fallback pattern, not a conflict.
func (s *ConflictingListenersTestSuite) TestPassesWithOneCatchAllAndOneHostname() {
	s.addGateway(
		listener("shop", 80, gatewayv1.HTTPProtocolType, hostPtr("shop.example.com")),
		listener("fallback", 80, gatewayv1.HTTPProtocolType, nil),
	)

	s.expect(nil)
}

func (s *ConflictingListenersTestSuite) TestPassesWhenThePortsDiffer() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-alt", 8080, gatewayv1.HTTPProtocolType, nil),
		listener("raw", 9000, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(nil)
}

// TCP and UDP on one port number are different sockets. DNS is configured this
// way on purpose, and flagging it would be a false positive.
func (s *ConflictingListenersTestSuite) TestPassesWithTCPAndUDPOnTheSamePort() {
	s.addGateway(
		listener("dns-tcp", 53, gatewayv1.TCPProtocolType, nil),
		listener("dns-udp", 53, gatewayv1.UDPProtocolType, nil),
	)

	s.expect(nil)
}

func (s *ConflictingListenersTestSuite) TestFlagsTwoRawTCPListenersOnOnePort() {
	s.addGateway(
		listener("db", 5432, gatewayv1.TCPProtocolType, nil),
		listener("db-copy", 5432, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinctWithoutHostname("Gateway", gatewayName,
			`listeners "db" and "db-copy"`, 5432, gatewayv1.TCPProtocolType)},
	})
}

func (s *ConflictingListenersTestSuite) TestFlagsTwoUDPListenersOnOnePort() {
	s.addGateway(
		listener("syslog", 514, gatewayv1.UDPProtocolType, nil),
		listener("syslog-copy", 514, gatewayv1.UDPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinctWithoutHostname("Gateway", gatewayName,
			`listeners "syslog" and "syslog-copy"`, 514, gatewayv1.UDPProtocolType)},
	})
}

// HTTPS beside TLS on one port is distinct per the spec, and whether an
// implementation can serve it is Extended support that varies. Guessing here
// would cost a false positive.
func (s *ConflictingListenersTestSuite) TestPassesWithHTTPSBesideTLSOnOnePort() {
	s.addGateway(
		listener("terminate", 443, gatewayv1.HTTPSProtocolType, hostPtr("shop.example.com")),
		listener("passthrough", 443, gatewayv1.TLSProtocolType, hostPtr("vpn.example.com")),
	)

	s.expect(nil)
}

// A protocol gwlint has no rule for is somebody else's extension, and its
// absence from the model says nothing about whether it can share a port.
func (s *ConflictingListenersTestSuite) TestPassesOnAnUnmodelledProtocol() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("custom", 80, gatewayv1.ProtocolType("vendor.io/QUIC"), nil),
	)

	s.expect(nil)
}

// Two listeners of the same unmodelled protocol are still indistinct, and one
// carrying a hostname gets the wording that mentions it.
func (s *ConflictingListenersTestSuite) TestFlagsIdenticalUnmodelledListeners() {
	s.addGateway(
		listener("custom", 80, gatewayv1.ProtocolType("vendor.io/QUIC"), hostPtr("edge.example.com")),
		listener("custom-copy", 80, gatewayv1.ProtocolType("vendor.io/QUIC"), hostPtr("edge.example.com")),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinct("Gateway", gatewayName,
			`listeners "custom" and "custom-copy"`, gatewayv1.ProtocolType("vendor.io/QUIC"),
			`hostname "edge.example.com"`)},
	})
}

func (s *ConflictingListenersTestSuite) TestReportsEachConflictingPortOnce() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-copy", 80, gatewayv1.HTTPProtocolType, nil),
		listener("db", 5432, gatewayv1.TCPProtocolType, nil),
		listener("db-copy", 5432, gatewayv1.TCPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {
			wantIndistinct("Gateway", gatewayName, `listeners "web" and "web-copy"`,
				gatewayv1.HTTPProtocolType, "no hostname, so they match every request"),
			wantIndistinctWithoutHostname("Gateway", gatewayName, `listeners "db" and "db-copy"`, 5432,
				gatewayv1.TCPProtocolType),
		},
	})
}

// Three listeners sharing everything are one finding naming all three, not
// three pairwise findings.
func (s *ConflictingListenersTestSuite) TestGroupsMoreThanTwoIndistinctListeners() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-copy", 80, gatewayv1.HTTPProtocolType, nil),
		listener("web-third", 80, gatewayv1.HTTPProtocolType, nil),
	)

	s.expect(map[string][]string{
		gatewayName: {wantIndistinct("Gateway", gatewayName,
			`listeners "web", "web-copy" and "web-third"`,
			gatewayv1.HTTPProtocolType, "no hostname, so they match every request")},
	})
}

func (s *ConflictingListenersTestSuite) TestPassesOnASingleListener() {
	s.addGateway(listener("web", 80, gatewayv1.HTTPProtocolType, nil))

	s.expect(nil)
}

func (s *ConflictingListenersTestSuite) TestPassesOnAGatewayWithNoListeners() {
	s.addGateway()

	s.expect(nil)
}

func (s *ConflictingListenersTestSuite) TestFlagsConflictsInAListenerSet() {
	s.addListenerSet("tenant-listeners",
		entry("tenant", 80, gatewayv1.HTTPProtocolType, hostPtr("tenant.example.com")),
		entry("tenant-copy", 80, gatewayv1.HTTPProtocolType, hostPtr("tenant.example.com")),
	)

	s.expect(map[string][]string{
		"tenant-listeners": {wantIndistinct("ListenerSet", "tenant-listeners",
			`listeners "tenant" and "tenant-copy"`, gatewayv1.HTTPProtocolType,
			`hostname "tenant.example.com"`)},
	})
}

// Listeners are judged within the object that declares them. A Gateway and a
// ListenerSet may each hold a listener on port 80 with no hostname, and
// whether the merged set conflicts is a separate question this check does not
// answer.
func (s *ConflictingListenersTestSuite) TestDoesNotCompareAcrossObjects() {
	s.addGateway(listener("web", 80, gatewayv1.HTTPProtocolType, nil))
	s.addListenerSet("extra-listeners", entry("web", 80, gatewayv1.HTTPProtocolType, nil))

	s.expect(nil)
}

func (s *ConflictingListenersTestSuite) TestIgnoresObjectsThatDeclareNoListeners() {
	s.addRoute("some-route")

	s.expect(nil)
}

// Nothing the check reads lives outside the object, so a Gateway linted on its
// own is judged in full. That is why there is no partial-set guard here.
func (s *ConflictingListenersTestSuite) TestJudgesAGatewayOnItsOwn() {
	s.addGateway(
		listener("web", 80, gatewayv1.HTTPProtocolType, nil),
		listener("raw", 80, gatewayv1.TCPProtocolType, nil),
	)

	s.Len(s.ctx.Objects(), 1)
	s.expect(map[string][]string{
		gatewayName: {wantProtocolConflict(
			`listener "web"`, gatewayv1.HTTPProtocolType,
			`listener "raw"`, gatewayv1.TCPProtocolType, 80, rawTCPReason)},
	})
}
