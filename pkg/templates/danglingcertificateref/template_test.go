package danglingcertificateref

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	gatewayName = "edge-gateway"
	gatewayNS   = "edge"
)

func TestDanglingCertificateRef(t *testing.T) {
	suite.Run(t, new(DanglingCertificateRefTestSuite))
}

type DanglingCertificateRefTestSuite struct {
	templates.TemplateTestSuite
	ctx *mocks.MockLintContext
}

func (s *DanglingCertificateRefTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func nsPtr(n gatewayv1.Namespace) *gatewayv1.Namespace       { return &n }
func kindPtr(k gatewayv1.Kind) *gatewayv1.Kind               { return &k }
func grpPtr(g gatewayv1.Group) *gatewayv1.Group              { return &g }
func modePtr(m gatewayv1.TLSModeType) *gatewayv1.TLSModeType { return &m }

func certRef(name string, ns *gatewayv1.Namespace) gatewayv1.SecretObjectReference {
	return gatewayv1.SecretObjectReference{Name: gatewayv1.ObjectName(name), Namespace: ns}
}

func (s *DanglingCertificateRefTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: gatewayNS},
		Spec:       gatewayv1.GatewaySpec{Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway", gw)
}

func (s *DanglingCertificateRefTestSuite) addSecret(name, ns string) {
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	secret.SetGroupVersionKind(schema.GroupVersion{Version: "v1"}.WithKind("Secret"))
	s.ctx.AddObject("secret-"+ns+"-"+name, secret)
}

func terminatingListener(name gatewayv1.SectionName, refs ...gatewayv1.SecretObjectReference) gatewayv1.Listener {
	return gatewayv1.Listener{
		Name: name, Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: refs},
	}
}

func (s *DanglingCertificateRefTestSuite) expect(messages ...string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for _, m := range messages {
		expected[gatewayName] = append(expected[gatewayName], diagnostic.Diagnostic{Message: m})
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

func want(listener, secret string) string {
	return fmt.Sprintf(
		"Gateway %q listener %q terminates TLS with Secret %q in namespace %q, which is not present; "+
			"the listener will never be programmed and TLS handshakes on it will fail",
		gatewayName, listener, secret, gatewayNS)
}

func (s *DanglingCertificateRefTestSuite) TestFlagsAMissingSecretInTheOwnNamespace() {
	s.addSecret("some-other-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("edge-cert", nil)))

	s.expect(want("https", "edge-cert"))
}

func (s *DanglingCertificateRefTestSuite) TestPassesWhenTheSecretExists() {
	s.addSecret("edge-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("edge-cert", nil)))

	s.expect()
}

// An omitted certificateRef namespace defaults to the Gateway's own, so a
// Secret of the same name sitting in another namespace does not satisfy it.
func (s *DanglingCertificateRefTestSuite) TestDefaultsToTheGatewayNamespace() {
	s.addSecret("some-other-cert", gatewayNS) // describes the gatewayNS namespace
	s.addSecret("edge-cert", "elsewhere")
	s.addGateway(terminatingListener("https", certRef("edge-cert", nil)))

	s.expect(want("https", "edge-cert"))
}

func (s *DanglingCertificateRefTestSuite) TestResolvesAnExplicitNamespace() {
	s.addSecret("edge-cert", "elsewhere")
	s.addGateway(terminatingListener("https", certRef("edge-cert", nsPtr("elsewhere"))))

	s.expect()
}

// Passthrough listeners never resolve certificateRefs at all, so a dangling
// one there breaks nothing.
func (s *DanglingCertificateRefTestSuite) TestIgnoresPassthroughListeners() {
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{
			Mode:            modePtr(gatewayv1.TLSModePassthrough),
			CertificateRefs: []gatewayv1.SecretObjectReference{certRef("edge-cert", nil)},
		},
	})

	s.expect()
}

// An omitted mode defaults to Terminate, so certificates still matter.
func (s *DanglingCertificateRefTestSuite) TestOmittedModeDefaultsToTerminate() {
	s.addSecret("some-other-cert", gatewayNS)
	s.addGateway(gatewayv1.Listener{
		Name: "https", Port: 443, Protocol: gatewayv1.HTTPSProtocolType,
		TLS: &gatewayv1.ListenerTLSConfig{CertificateRefs: []gatewayv1.SecretObjectReference{certRef("edge-cert", nil)}},
	})

	s.expect(want("https", "edge-cert"))
}

// A ref into some other implementation's certificate kind is not something
// gwlint has typed knowledge of, so its absence here says nothing.
func (s *DanglingCertificateRefTestSuite) TestIgnoresUnknownCertificateKinds() {
	s.addSecret("some-other-cert", gatewayNS)
	ref := certRef("vault-cert", nil)
	ref.Group = grpPtr("vault.hashicorp.com")
	ref.Kind = kindPtr("VaultCertificate")
	s.addGateway(terminatingListener("https", ref))

	s.expect()
}

// The guard that matters: a namespace this manifest set says nothing about is
// not a namespace whose Secrets gwlint can judge. Certificates are routinely
// rendered by a different chart or issued by cert-manager and never
// committed at all.
func (s *DanglingCertificateRefTestSuite) TestStaysQuietForNamespacesTheSetDoesNotDescribe() {
	s.addGateway(terminatingListener("https", certRef("edge-cert", nsPtr("cert-manager"))))

	s.expect()
}

// And a Secret existing in one namespace must not license judging every other.
func (s *DanglingCertificateRefTestSuite) TestJudgesOnlyNamespacesDescribedBySecrets() {
	s.addSecret("unrelated-secret", "some-namespace")
	s.addGateway(terminatingListener("https", certRef("edge-cert", nsPtr("cert-manager"))))

	s.expect()
}

func (s *DanglingCertificateRefTestSuite) TestReportsEveryDanglingRefOnAGateway() {
	s.addSecret("real-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("real-cert", nil), certRef("missing-one", nil), certRef("missing-two", nil)))

	s.expect(want("https", "missing-one"), want("https", "missing-two"))
}

func (s *DanglingCertificateRefTestSuite) TestReportsAcrossMultipleListeners() {
	s.addSecret("real-cert", gatewayNS)
	s.addGateway(
		terminatingListener("https-a", certRef("missing-a", nil)),
		terminatingListener("https-b", certRef("real-cert", nil)),
	)

	s.expect(want("https-a", "missing-a"))
}

// addCertManagerCertificate adds a cert-manager Certificate naming a Secret
// that is not rendered directly: cert-manager's controller creates it at
// runtime from this object, so its absence from the manifest set is not
// evidence it is missing from the cluster.
func (s *DanglingCertificateRefTestSuite) addCertManagerCertificate(secretName, ns string) {
	cert := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "cert-manager.io/v1",
		"kind":       "Certificate",
		"metadata":   map[string]interface{}{"name": secretName, "namespace": ns},
		"spec":       map[string]interface{}{"secretName": secretName},
	}}
	s.ctx.AddObject("certificate-"+ns+"-"+secretName, cert)
}

// A cert-manager Certificate is the common way a listener's TLS Secret comes
// to exist without ever being rendered as a Secret manifest itself: the
// controller creates it from the Certificate object at apply time.
func (s *DanglingCertificateRefTestSuite) TestPassesWhenACertManagerCertificateNamesTheSecret() {
	s.addCertManagerCertificate("edge-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("edge-cert", nil)))

	s.expect()
}

// The Certificate has to actually name the Secret gwlint is looking for; one
// naming something else does not vouch for an unrelated reference.
func (s *DanglingCertificateRefTestSuite) TestDoesNotAcceptAnUnrelatedCertificate() {
	s.addCertManagerCertificate("some-other-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("edge-cert", nil)))

	s.expect(want("https", "edge-cert"))
}

// A namespace described only by a cert-manager Certificate (no rendered
// Secret at all) still counts as described, and the Certificate's own
// secretName is what resolves the reference, not the fact that some Secret
// exists in the namespace generally.
func (s *DanglingCertificateRefTestSuite) TestCertManagerCertificateAloneDescribesTheNamespace() {
	s.addCertManagerCertificate("edge-cert", gatewayNS)
	s.addGateway(terminatingListener("https", certRef("missing-cert", nil)))

	s.expect(want("https", "missing-cert"))
}
