package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// certManagerGroup is cert-manager's API group. Matched on group and kind
// alone, not a specific version: cert-manager's Certificate has been served
// at v1 for years, but older manifests still turn up at v1alpha2 or v1alpha3,
// and none of gwlint's own code needs anything from the type beyond the two
// fields read below.
const certManagerGroup = "cert-manager.io"

// SecretKind is what a listener certificateRef resolves to unless it says
// otherwise, per Gateway API's default on SecretObjectReference.Kind.
const SecretKind = "Secret"

// CertificateRef is one listener certificateRef with Gateway API's defaults
// applied: the core API group, kind Secret, and the namespace of the Gateway
// or ListenerSet doing the referring.
type CertificateRef struct {
	// Owner is the Gateway or ListenerSet declaring the listener, which is
	// also the "from" side a ReferenceGrant has to name.
	Owner    ObjectRef
	Listener gatewayv1.SectionName

	Group     string
	Kind      string
	Namespace string
	Name      string
}

// TerminatingCertificateRefs returns the certificateRefs of every TLS listener
// on obj that terminates TLS, or nil if obj is not a Gateway or ListenerSet.
// Passthrough listeners are skipped: Gateway API ignores their certificateRefs
// entirely, so a ref that resolves to nothing there breaks nothing.
//
// This walks listeners itself rather than going through ListenersOf, because
// the Listener model deliberately carries only the fields route attachment
// turns on, and TLS is not one of them.
func TerminatingCertificateRefs(obj k8sutil.Object) []CertificateRef {
	if gw, ok := AsGateway(obj); ok {
		owner := ObjectRef{Kind: gwobjectkinds.Gateway, Namespace: gw.Namespace, Name: gw.Name}
		var refs []CertificateRef
		for _, listener := range gw.Spec.Listeners {
			refs = append(refs, certificateRefsOf(owner, listener.Name, listener.TLS)...)
		}
		return refs
	}
	if ls, ok := obj.(*gatewayv1.ListenerSet); ok {
		owner := ObjectRef{Kind: gwobjectkinds.ListenerSet, Namespace: ls.Namespace, Name: ls.Name}
		var refs []CertificateRef
		for _, listener := range ls.Spec.Listeners {
			refs = append(refs, certificateRefsOf(owner, listener.Name, listener.TLS)...)
		}
		return refs
	}
	return nil
}

func certificateRefsOf(owner ObjectRef, listener gatewayv1.SectionName, tls *gatewayv1.ListenerTLSConfig) []CertificateRef {
	// An omitted mode is Terminate, so the certificates matter by default.
	if tls == nil || (tls.Mode != nil && *tls.Mode != gatewayv1.TLSModeTerminate) {
		return nil
	}
	refs := make([]CertificateRef, 0, len(tls.CertificateRefs))
	for _, ref := range tls.CertificateRefs {
		cert := CertificateRef{
			Owner:     owner,
			Listener:  listener,
			Kind:      SecretKind,
			Namespace: owner.Namespace,
			Name:      string(ref.Name),
		}
		if ref.Group != nil {
			cert.Group = string(*ref.Group)
		}
		if ref.Kind != nil {
			cert.Kind = string(*ref.Kind)
		}
		if ref.Namespace != nil {
			cert.Namespace = string(*ref.Namespace)
		}
		refs = append(refs, cert)
	}
	return refs
}

// ResolvableCertificateKind reports whether gwlint can tell that a
// certificateRef of this group and kind resolves to nothing. Gateway API
// allows implementation-specific certificate kinds, and the absence of
// somebody else's CRD from a manifest set says nothing about the cluster.
func ResolvableCertificateKind(group, kind string) bool {
	return group == "" && kind == SecretKind
}

// CertificateSecrets indexes every Secret a listener certificateRef can
// resolve to: every core Secret in the context directly, plus every Secret a
// cert-manager Certificate names as its spec.secretName in the same
// namespace. cert-manager's controller creates that Secret at runtime from
// the Certificate object, never as a chart-rendered manifest of its own, so a
// render carrying the Certificate but not the Secret is not missing anything;
// treating the certificateRef as dangling there would be gwlint's mistake,
// not the manifest's.
func CertificateSecrets(lintCtx lintcontext.LintContext) map[ObjectRef]struct{} {
	found := make(map[ObjectRef]struct{})
	for _, obj := range lintCtx.Objects() {
		switch typed := obj.K8sObject.(type) {
		case *corev1.Secret:
			found[ObjectRef{Kind: SecretKind, Namespace: typed.Namespace, Name: typed.Name}] = struct{}{}
		case *unstructured.Unstructured:
			if name, ok := certManagerSecretName(typed); ok {
				found[ObjectRef{Kind: SecretKind, Namespace: typed.GetNamespace(), Name: name}] = struct{}{}
			}
		}
	}
	return found
}

// certManagerSecretName returns the Secret name a cert-manager Certificate
// object will populate, and reports whether obj was such an object.
// Certificate is not one of gwlint's registered kinds, so kube-linter's
// decoder falls back to unstructured for it; gwlint reads the two fields it
// needs directly rather than adding a dependency on cert-manager's own types.
func certManagerSecretName(obj *unstructured.Unstructured) (string, bool) {
	gvk := obj.GroupVersionKind()
	if gvk.Group != certManagerGroup || gvk.Kind != "Certificate" {
		return "", false
	}
	name, found, err := unstructured.NestedString(obj.Object, "spec", "secretName")
	if err != nil || !found || name == "" {
		return "", false
	}
	return name, true
}
