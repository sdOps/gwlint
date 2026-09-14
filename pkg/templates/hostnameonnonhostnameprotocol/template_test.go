package hostnameonnonhostnameprotocol

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

const gatewayName = "db-gateway"

func TestHostnameOnNonHostnameProtocol(t *testing.T) {
	suite.Run(t, new(HostnameOnNonHostnameProtocolTestSuite))
}

type HostnameOnNonHostnameProtocolTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *HostnameOnNonHostnameProtocolTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func hostPtr(h gatewayv1.Hostname) *gatewayv1.Hostname { return &h }

func (s *HostnameOnNonHostnameProtocolTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: "default"},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(
		schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("Gateway"))
	s.ctx.AddObject("gateway", gw)
}

func (s *HostnameOnNonHostnameProtocolTestSuite) expect(messages ...string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for _, m := range messages {
		expected[gatewayName] = append(expected[gatewayName], diagnostic.Diagnostic{Message: m})
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func want(listener string, protocol gatewayv1.ProtocolType, port int) string {
	return fmt.Sprintf(
		"Gateway %q listener %q sets hostname \"db.internal\" on protocol %s, which has neither SNI "+
			"nor a Host header to match it; Gateway API ignores the hostname and the listener still "+
			"admits every connection on port %d",
		gatewayName, listener, protocol, port)
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestFlagsHostnameOnTCP() {
	s.addGateway(gatewayv1.Listener{
		Name: "db", Port: 5432, Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr("db.internal"),
	})

	s.expect(want("db", gatewayv1.TCPProtocolType, 5432))
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestFlagsHostnameOnUDP() {
	s.addGateway(gatewayv1.Listener{
		Name: "db", Port: 5432, Protocol: gatewayv1.UDPProtocolType, Hostname: hostPtr("db.internal"),
	})

	s.expect(want("db", gatewayv1.UDPProtocolType, 5432))
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestPassesWhenNoHostnameIsSet() {
	s.addGateway(gatewayv1.Listener{Name: "db", Port: 5432, Protocol: gatewayv1.TCPProtocolType})

	s.expect()
}

// The CRD's own validation rule treats an empty string the same as unset.
func (s *HostnameOnNonHostnameProtocolTestSuite) TestPassesWhenHostnameIsEmptyString() {
	s.addGateway(gatewayv1.Listener{
		Name: "db", Port: 5432, Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr(""),
	})

	s.expect()
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestPassesForHTTP() {
	s.addGateway(gatewayv1.Listener{
		Name: "web", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostPtr("example.com"),
	})

	s.expect()
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestPassesForHTTPS() {
	s.addGateway(gatewayv1.Listener{
		Name: "web", Port: 443, Protocol: gatewayv1.HTTPSProtocolType, Hostname: hostPtr("example.com"),
	})

	s.expect()
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestPassesForTLS() {
	s.addGateway(gatewayv1.Listener{
		Name: "web", Port: 443, Protocol: gatewayv1.TLSProtocolType, Hostname: hostPtr("example.com"),
	})

	s.expect()
}

func (s *HostnameOnNonHostnameProtocolTestSuite) TestReportsEveryOffendingListener() {
	s.addGateway(
		gatewayv1.Listener{Name: "db", Port: 5432, Protocol: gatewayv1.TCPProtocolType, Hostname: hostPtr("db.internal")},
		gatewayv1.Listener{Name: "web", Port: 80, Protocol: gatewayv1.HTTPProtocolType, Hostname: hostPtr("example.com")},
		gatewayv1.Listener{Name: "cache", Port: 6379, Protocol: gatewayv1.UDPProtocolType, Hostname: hostPtr("db.internal")},
	)

	s.expect(want("db", gatewayv1.TCPProtocolType, 5432), want("cache", gatewayv1.UDPProtocolType, 6379))
}
