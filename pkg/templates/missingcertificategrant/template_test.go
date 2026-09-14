package missingcertificategrant

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
)

const (
	gatewayName = "storefront-gateway"
	gatewayNS   = "storefront"
	certNS      = "certs"
)

func TestMissingCertificateGrant(t *testing.T) {
	suite.Run(t, new(MissingCertificateGrantTestSuite))
}

type MissingCertificateGrantTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *MissingCertificateGrantTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace     { return &n }
func namePtr(n gatewayv1.ObjectName) *gatewayv1.ObjectName { return &n }
func certRef(name string, ns gatewayv1.Namespace) gatewayv1.SecretObjectReference {
	return gatewayv1.SecretObjectReference{Name: gatewayv1.ObjectName(name), Namespace: nsPtr(ns)}
}

func (s *MissingCertificateGrantTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: gatewayNS},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway", gw)
}

func (s *MissingCertificateGrantTestSuite) addSecret(name, ns string) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	secret.SetGroupVersionKind(schema.GroupVersion{Version: "v1"}.WithKind("Secret"))
	s.ctx.AddObject("secret-"+ns+"-"+name, secret)
}

func (s *MissingCertificateGrantTestSuite) addGrant(from gatewayv1.ReferenceGrantFrom, to gatewayv1.ReferenceGrantTo) {
	rg := &gatewayv1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{Name: "grant", Namespace: certNS},
		Spec:       gatewayv1.ReferenceGrantSpec{From: []gatewayv1.ReferenceGrantFrom{from}, To: []gatewayv1.ReferenceGrantTo{to}},
	}
	rg.SetGroupVersionKind(gvk("ReferenceGrant"))
	s.ctx.AddObject("grant-"+certNS, rg)
}

func terminatingListener(refs ...gatewayv1.SecretObjectReference) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: refs},
	}
}

func gatewayFrom() gatewayv1.ReferenceGrantFrom {
	return gatewayv1.ReferenceGrantFrom{Group: gatewayv1.GroupName, Kind: "Gateway", Namespace: gatewayv1.Namespace(gatewayNS)}
}

func secretTo(name *gatewayv1.ObjectName) gatewayv1.ReferenceGrantTo {
	return gatewayv1.ReferenceGrantTo{Kind: "Secret", Name: name}
}

func (s *MissingCertificateGrantTestSuite) expect(messages ...string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for _, m := range messages {
		expected[gatewayName] = append(expected[gatewayName], diagnostic.Diagnostic{Message: m})
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func want() string {
	return fmt.Sprintf(
		"Gateway %q listener %q terminates TLS with Secret \"storefront-cert\" in namespace %q, "+
			"which no ReferenceGrant there permits it to reference; the listener will never be "+
			"programmed and TLS handshakes on it will fail",
		gatewayName, "https", certNS)
}

func (s *MissingCertificateGrantTestSuite) TestFlagsACrossNamespaceRefWithNoGrant() {
	s.addSecret("storefront-cert", certNS) // describes certNS, but grants no access
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect(want())
}

func (s *MissingCertificateGrantTestSuite) TestPassesWithAMatchingGrant() {
	s.addSecret("storefront-cert", certNS)
	s.addGrant(gatewayFrom(), secretTo(nil))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect()
}

// A grant naming a specific Secret only covers that one.
func (s *MissingCertificateGrantTestSuite) TestGrantNamedToASpecificSecretCoversOnlyThat() {
	s.addSecret("storefront-cert", certNS)
	s.addGrant(gatewayFrom(), secretTo(namePtr("some-other-cert")))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect(want())
}

func (s *MissingCertificateGrantTestSuite) TestGrantNamedToTheRightSecretPasses() {
	s.addSecret("storefront-cert", certNS)
	s.addGrant(gatewayFrom(), secretTo(namePtr("storefront-cert")))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect()
}

// A grant naming a different "from" namespace does not cover this Gateway.
func (s *MissingCertificateGrantTestSuite) TestGrantFromTheWrongNamespaceDoesNotApply() {
	s.addSecret("storefront-cert", certNS)
	from := gatewayFrom()
	from.Namespace = "some-other-namespace"
	s.addGrant(from, secretTo(nil))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect(want())
}

// A reference within the listener's own namespace needs no grant at all.
func (s *MissingCertificateGrantTestSuite) TestPassesWithinTheSameNamespace() {
	s.addGateway(terminatingListener(certRef("storefront-cert", gatewayNS)))

	s.expect()
}

// Per GEP-1713, a grant is not inherited between a Gateway and a ListenerSet:
// the grant has to name the kind that actually declares the listener.
func (s *MissingCertificateGrantTestSuite) TestListenerSetFromDoesNotSatisfyAGatewayReference() {
	s.addSecret("storefront-cert", certNS)
	from := gatewayFrom()
	from.Kind = "ListenerSet"
	s.addGrant(from, secretTo(nil))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect(want())
}

// The guard that matters: a namespace this manifest set says nothing about
// (no Secret and no ReferenceGrant there) is not one gwlint can judge.
func (s *MissingCertificateGrantTestSuite) TestStaysQuietForNamespacesTheSetDoesNotDescribe() {
	s.addGateway(terminatingListener(certRef("storefront-cert", "cert-manager")))

	s.expect()
}

// A ReferenceGrant alone, with no Secret present, still describes the
// namespace enough to judge the reference.
func (s *MissingCertificateGrantTestSuite) TestAGrantAloneDescribesTheNamespace() {
	from := gatewayFrom()
	s.addGrant(from, secretTo(namePtr("some-other-cert")))
	s.addGateway(terminatingListener(certRef("storefront-cert", certNS)))

	s.expect(want())
}

func (s *MissingCertificateGrantTestSuite) TestReportsEveryUngrantedRefOnAGateway() {
	s.addSecret("storefront-cert", certNS)
	s.addSecret("payments-cert", "payments")
	s.addGateway(terminatingListener(
		certRef("storefront-cert", certNS),
		certRef("payments-cert", "payments"),
	))

	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: map[string][]diagnostic.Diagnostic{
		gatewayName: {
			{Message: want()},
			{Message: fmt.Sprintf(
				"Gateway %q listener %q terminates TLS with Secret \"payments-cert\" in namespace %q, "+
					"which no ReferenceGrant there permits it to reference; the listener will never be "+
					"programmed and TLS handshakes on it will fail",
				gatewayName, "https", "payments")},
		},
	}}})
}
