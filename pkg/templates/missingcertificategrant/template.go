// Package missingcertificategrant flags a TLS listener whose certificateRefs
// reach into another namespace with no ReferenceGrant permitting it. Gateway
// API deliberately makes the owner of the certificate opt in: without a grant
// in the Secret's namespace the listener gets ResolvedRefs=False with reason
// RefNotPermitted and is never programmed, even though the Secret exists and
// the name is spelt correctly. Nothing about either manifest is invalid, so a
// schema validator cannot see it, and the failure looks like a certificate
// problem rather than a permission one.
//
// A reference is judged only when the manifest set describes the Secret's
// namespace, meaning that namespace holds at least one Secret or
// ReferenceGrant here. A grant lives next to the certificate it protects and
// is usually owned by whoever owns that namespace, so a set that carries
// neither says nothing about whether the grant exists.
//
// Per GEP-1713 a grant is not inherited in either direction between a Gateway
// and its ListenerSets, so the grant's from.kind has to name the kind that
// declares the listener, not just Gateway.
package missingcertificategrant

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "missing-certificate-grant"

func init() {
	templates.Register(check.Template{
		HumanName: "Missing certificate ReferenceGrant",
		Key:       templateKey,
		Description: "Flag TLS listeners whose certificateRefs cross into another namespace with no " +
			"ReferenceGrant permitting it, so the listener is never programmed",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.Gateway, gwobjectkinds.ListenerSet},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

// grant is a ReferenceGrant reduced to what deciding a reference needs: the
// namespace it lives in, which is the namespace it protects, plus its spec.
type grant struct {
	namespace string
	spec      gatewayv1.ReferenceGrantSpec
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	certificateRefs := gatewayapi.TerminatingCertificateRefs(object.K8sObject)
	if len(certificateRefs) == 0 {
		return nil
	}

	grants := referenceGrants(lintCtx)
	described := describedNamespaces(lintCtx, grants)

	var diagnostics []diagnostic.Diagnostic
	for _, ref := range certificateRefs {
		// A reference within the listener's own namespace needs no grant.
		if ref.Namespace == ref.Owner.Namespace {
			continue
		}
		if !described[ref.Namespace] {
			continue
		}
		if permitted(grants, ref) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q listener %q terminates TLS with %s %q in namespace %q, which no ReferenceGrant there "+
					"permits it to reference; the listener will never be programmed and TLS handshakes on it will fail",
				ref.Owner.Kind, ref.Owner.Name, ref.Listener, ref.Kind, ref.Name, ref.Namespace),
		})
	}
	return diagnostics
}

// permitted reports whether any grant in the referenced namespace allows this
// listener's owner to use the certificate.
func permitted(grants []grant, ref gatewayapi.CertificateRef) bool {
	for _, g := range grants {
		if g.namespace != ref.Namespace {
			continue
		}
		if grantAllowsFrom(g.spec.From, ref.Owner) && grantAllowsTo(g.spec.To, ref) {
			return true
		}
	}
	return false
}

func grantAllowsFrom(from []gatewayv1.ReferenceGrantFrom, owner gatewayapi.ObjectRef) bool {
	for _, f := range from {
		if string(f.Group) == gatewayapi.GroupName &&
			string(f.Kind) == owner.Kind &&
			string(f.Namespace) == owner.Namespace {
			return true
		}
	}
	return false
}

func grantAllowsTo(to []gatewayv1.ReferenceGrantTo, ref gatewayapi.CertificateRef) bool {
	for _, t := range to {
		if string(t.Group) != ref.Group || string(t.Kind) != ref.Kind {
			continue
		}
		// An omitted name covers every object of that group and kind in the
		// namespace.
		if t.Name == nil || string(*t.Name) == ref.Name {
			return true
		}
	}
	return false
}

// referenceGrants collects every ReferenceGrant in the context. ReferenceGrant
// is served at three versions and usually authored as v1beta1; the v1beta1 and
// v1alpha2 types are defined over v1's, so each converts directly.
func referenceGrants(lintCtx lintcontext.LintContext) []grant {
	var grants []grant
	for _, obj := range lintCtx.Objects() {
		var rg *gatewayv1.ReferenceGrant
		switch typed := obj.K8sObject.(type) {
		case *gatewayv1.ReferenceGrant:
			rg = typed
		case *gatewayv1b1.ReferenceGrant:
			rg = (*gatewayv1.ReferenceGrant)(typed)
		case *gatewayv1a2.ReferenceGrant:
			rg = (*gatewayv1.ReferenceGrant)(typed)
		default:
			continue
		}
		grants = append(grants, grant{namespace: rg.Namespace, spec: rg.Spec})
	}
	return grants
}

// describedNamespaces returns the namespaces this manifest set says enough
// about to judge a certificate reference into them: ones that carry a Secret
// or a ReferenceGrant of their own.
func describedNamespaces(lintCtx lintcontext.LintContext, grants []grant) map[string]bool {
	described := map[string]bool{}
	for ref := range gatewayapi.CertificateSecrets(lintCtx) {
		described[ref.Namespace] = true
	}
	for _, g := range grants {
		described[g.namespace] = true
	}
	return described
}
