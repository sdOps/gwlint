package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

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

// CertificateSecrets indexes every core Secret in the context, which is how a
// check decides whether a listener certificateRef resolves to anything.
func CertificateSecrets(lintCtx lintcontext.LintContext) map[ObjectRef]struct{} {
	found := make(map[ObjectRef]struct{})
	for _, obj := range lintCtx.Objects() {
		if _, ok := obj.K8sObject.(*corev1.Secret); !ok {
			continue
		}
		found[ObjectRef{
			Kind:      SecretKind,
			Namespace: obj.K8sObject.GetNamespace(),
			Name:      obj.K8sObject.GetName(),
		}] = struct{}{}
	}
	return found
}
