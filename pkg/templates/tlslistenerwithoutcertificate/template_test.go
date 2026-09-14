package tlslistenerwithoutcertificate

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
)

const (
	gatewayName = "edge-gateway"
	namespace   = "default"
)

func TestTLSListenerWithoutCertificate(t *testing.T) {
	suite.Run(t, new(TLSListenerWithoutCertificateTestSuite))
}

type TLSListenerWithoutCertificateTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *TLSListenerWithoutCertificateTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func modePtr(m gatewayv1.TLSModeType) *gatewayv1.TLSModeType { return &m }

func (s *TLSListenerWithoutCertificateTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg", Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject(gatewayName, gw)
}

func (s *TLSListenerWithoutCertificateTestSuite) addV1Beta1Gateway(name string, listeners ...gatewayv1.Listener) {
	gw := &gatewayv1b1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg", Listeners: listeners},
	}
	gw.SetGroupVersionKind(schema.GroupVersion{
		Group: gatewayv1.GroupName, Version: gatewayv1b1.GroupVersion.Version,
	}.WithKind("Gateway"))
	s.ctx.AddObject(name, gw)
}

func certRef(name string) gatewayv1.SecretObjectReference {
	return gatewayv1.SecretObjectReference{Name: gatewayv1.ObjectName(name)}
}

func want(gateway, listener string, protocol gatewayv1.ProtocolType, port int32) string {
	return fmt.Sprintf(
		"Gateway %q listener %q terminates %s but declares no tls.certificateRefs, "+
			"so it has no certificate to present and every handshake on port %d fails",
		gateway, listener, protocol, port)
}

func (s *TLSListenerWithoutCertificateTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// The common shape of the mistake, and the one the CRD's own CEL rules let
// through: HTTPS with no tls block at all.
func (s *TLSListenerWithoutCertificateTestSuite) TestFlagsHTTPSListenerWithNoTLSBlock() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
	})

	s.expect(map[string][]string{
		gatewayName: {want(gatewayName, "https", gatewayv1.HTTPSProtocolType, 443)},
	})
}

func (s *TLSListenerWithoutCertificateTestSuite) TestFlagsExplicitTerminateWithNoCertificateRefs() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{Mode: modePtr(gatewayv1.TLSModeTerminate)},
	})

	s.expect(map[string][]string{
		gatewayName: {want(gatewayName, "https", gatewayv1.HTTPSProtocolType, 443)},
	})
}

// An empty mode string is how the CRD spells "defaulted", so it is Terminate.
func (s *TLSListenerWithoutCertificateTestSuite) TestFlagsEmptyModeAsTerminate() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{Mode: modePtr("")},
	})

	s.expect(map[string][]string{
		gatewayName: {want(gatewayName, "https", gatewayv1.HTTPSProtocolType, 443)},
	})
}

func (s *TLSListenerWithoutCertificateTestSuite) TestFlagsTerminatingTLSProtocolListener() {
	s.addGateway(gatewayv1.Listener{
		Name: "passthrough-port", Port: 8443, Protocol: gatewayv1.TLSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{Mode: modePtr(gatewayv1.TLSModeTerminate)},
	})

	s.expect(map[string][]string{
		gatewayName: {want(gatewayName, "passthrough-port", gatewayv1.TLSProtocolType, 8443)},
	})
}

func (s *TLSListenerWithoutCertificateTestSuite) TestPassesWhenACertificateIsReferenced() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{
			CertificateRefs: []gatewayv1.SecretObjectReference{certRef("edge-cert")},
		},
	})

	s.expect(nil)
}

// Passthrough never deciphers the stream, so holding no certificate is the
// point rather than a mistake.
func (s *TLSListenerWithoutCertificateTestSuite) TestPassesPassthroughWithoutCertificates() {
	s.addGateway(gatewayv1.Listener{
		Name: "passthrough", Port: 8443, Protocol: gatewayv1.TLSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{Mode: modePtr(gatewayv1.TLSModePassthrough)},
	})

	s.expect(nil)
}

// Gateway API lets an implementation supply certificates through its own
// options, so options without certificateRefs is not something to guess at.
func (s *TLSListenerWithoutCertificateTestSuite) TestPassesWhenTLSOptionsAreSet() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{
			Options: map[gatewayv1.AnnotationKey]gatewayv1.AnnotationValue{
				"example.com/certificate-source": "vault",
			},
		},
	})

	s.expect(nil)
}

func (s *TLSListenerWithoutCertificateTestSuite) TestIgnoresProtocolsThatDoNotTerminateTLS() {
	s.addGateway(
		gatewayv1.Listener{Name: "http", Port: 80, Protocol: gatewayv1.HTTPProtocolType},
		gatewayv1.Listener{Name: "postgres", Port: 5432, Protocol: gatewayv1.TCPProtocolType},
		gatewayv1.Listener{Name: "syslog", Port: 514, Protocol: gatewayv1.UDPProtocolType},
	)

	s.expect(nil)
}

// The Secret a certificateRef names usually lives in another chart, and its
// absence is a dangling reference for another check to judge, not a listener
// without a certificate.
func (s *TLSListenerWithoutCertificateTestSuite) TestPassesWhenTheReferencedSecretIsNotInTheSet() {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "some-other-cert", Namespace: namespace}}
	secret.SetGroupVersionKind(schema.GroupVersion{Version: "v1"}.WithKind("Secret"))
	s.ctx.AddObject("some-other-cert", secret)

	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{
			CertificateRefs: []gatewayv1.SecretObjectReference{certRef("cert-from-another-repo")},
		},
	})

	s.expect(nil)
}

func (s *TLSListenerWithoutCertificateTestSuite) TestReportsEveryUncoveredListener() {
	s.addGateway(
		gatewayv1.Listener{Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType},
		gatewayv1.Listener{Name: "grpcs", Port: 8443, Protocol: gatewayv1.HTTPSProtocolType},
		gatewayv1.Listener{
			Name: "secure-tcp", Port: 9443, Protocol: gatewayv1.TLSProtocolType,
			TLS: &gatewayv1.ListenerTLSConfig{
				CertificateRefs: []gatewayv1.SecretObjectReference{certRef("edge-cert")},
			},
		},
	)

	s.expect(map[string][]string{
		gatewayName: {
			want(gatewayName, "https", gatewayv1.HTTPSProtocolType, 443),
			want(gatewayName, "grpcs", gatewayv1.HTTPSProtocolType, 8443),
		},
	})
}

// A Gateway authored as v1beta1 decodes into its own Go type, and the object
// kind matches on group and kind at any version, so it has to be judged too.
func (s *TLSListenerWithoutCertificateTestSuite) TestJudgesV1Beta1Gateways() {
	s.addV1Beta1Gateway("legacy-gateway", gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
	})

	s.expect(map[string][]string{
		"legacy-gateway": {want("legacy-gateway", "https", gatewayv1.HTTPSProtocolType, 443)},
	})
}

func (s *TLSListenerWithoutCertificateTestSuite) TestIgnoresObjectsThatAreNotGateways() {
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "checkout", Namespace: namespace}}
	svc.SetGroupVersionKind(schema.GroupVersion{Version: "v1"}.WithKind("Service"))
	s.ctx.AddObject("checkout", svc)

	s.expect(nil)
}

// A Gateway with no listeners at all says nothing about TLS.
func (s *TLSListenerWithoutCertificateTestSuite) TestIgnoresGatewayWithNoListeners() {
	s.addGateway()

	s.expect(nil)
}

// The listener model has to keep the tls block: dropping it is what would make
// every terminating listener look uncovered.
func (s *TLSListenerWithoutCertificateTestSuite) TestListenerModelKeepsTheTLSBlock() {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "model-gateway", Namespace: namespace},
		Spec: gatewayv1.GatewaySpec{Listeners: []gatewayv1.Listener{{
			Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
			TLS: &gatewayv1.ListenerTLSConfig{
				CertificateRefs: []gatewayv1.SecretObjectReference{certRef("edge-cert")},
			},
		}}},
	}
	listeners, ok := gatewayapi.GatewayListenersOf(gw)
	s.Require().True(ok)
	s.Require().Len(listeners, 1)
	s.Require().NotNil(listeners[0].TLS)
	s.Equal(gatewayv1.TLSModeTerminate, listeners[0].TLSMode())
	s.Len(listeners[0].TLS.CertificateRefs, 1)
}
