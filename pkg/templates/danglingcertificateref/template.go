// Package danglingcertificateref flags a TLS listener whose certificateRefs
// name a Secret that is not present. Gateway API resolves a certificateRef
// when the Gateway is programmed, not when it is applied: a misspelt Secret
// name leaves the manifest valid, the apply clean, and the listener with
// ResolvedRefs=False, so every TLS handshake on that port fails. A schema
// validator sees a well-formed reference and has nothing to compare it with.
//
// A reference is judged only when the manifest set describes the namespace the
// Secret would live in, meaning that namespace already holds at least one
// Secret. Certificates are routinely rendered by a different chart, or issued
// into the cluster by cert-manager and never committed at all, so treating an
// absent namespace as an absent Secret would flag half the Gateways people own.
package danglingcertificateref

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "dangling-certificate-ref"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling listener certificateRef",
		Key:       templateKey,
		Description: "Flag TLS listeners whose certificateRefs name a Secret that is not present, " +
			"so the listener is never programmed and TLS handshakes on it fail",
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

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	certificateRefs := gatewayapi.TerminatingCertificateRefs(object.K8sObject)
	if len(certificateRefs) == 0 {
		return nil
	}

	known := gatewayapi.CertificateSecrets(lintCtx)
	// A namespace holding no Secret at all is a namespace this manifest set
	// does not describe, so its Secrets are not ours to judge.
	described := map[string]bool{}
	for ref := range known {
		described[ref.Namespace] = true
	}

	var diagnostics []diagnostic.Diagnostic
	for _, ref := range certificateRefs {
		if !gatewayapi.ResolvableCertificateKind(ref.Group, ref.Kind) {
			continue
		}
		if !described[ref.Namespace] {
			continue
		}
		if _, found := known[gatewayapi.ObjectRef{
			Kind: ref.Kind, Namespace: ref.Namespace, Name: ref.Name,
		}]; found {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"%s %q listener %q terminates TLS with Secret %q in namespace %q, which is not present; "+
					"the listener will never be programmed and TLS handshakes on it will fail",
				ref.Owner.Kind, ref.Owner.Name, ref.Listener, ref.Name, ref.Namespace),
		})
	}
	return diagnostics
}
