// Package tlslistenerwithoutcertificate flags a Gateway listener that is meant
// to terminate TLS but has no certificate to terminate it with. A listener on
// HTTPS or TLS in Terminate mode needs at least one tls.certificateRefs entry:
// without one there is nothing to present in the handshake, so every
// connection to that port fails while the Gateway itself applies cleanly.
//
// Terminate is what an unset tls.mode means, and an HTTPS listener that omits
// the tls block entirely is the usual shape of this mistake. Schema validation
// does not catch that shape at all. The CRD's CEL rule only demands
// certificateRefs once a tls block exists, so an HTTPS listener carrying no
// tls block passes even the apiserver, and offline validators such as
// kubeconform never evaluate CEL rules in the first place.
//
// Passthrough listeners are left alone: the Gateway forwards the TLS stream
// without deciphering it and holds no certificate by design. So is a listener
// that sets tls.options with no certificateRefs, which Gateway API allows so
// an implementation can source certificates its own way; gwlint does not know
// that implementation's semantics and would only guess.
//
// The check reads a single Gateway and resolves no references, so a partial
// manifest set needs no guard: the finding depends on nothing outside the
// object. A certificateRef naming a Secret absent from the set is deliberately
// not a finding here, since that is a dangling reference rather than a missing
// certificate, and the Secret usually lives in another repo.
package tlslistenerwithoutcertificate

import (
	"fmt"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "tls-listener-without-certificate"

func init() {
	templates.Register(check.Template{
		HumanName: "TLS listener without certificate",
		Key:       templateKey,
		Description: "Flag Gateway listeners that terminate TLS with no tls.certificateRefs, " +
			"so the listener has no certificate to present and every handshake fails",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.Gateway},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	listeners, ok := gatewayapi.GatewayListenersOf(object.K8sObject)
	if !ok {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, listener := range listeners {
		if !terminatesTLS(listener) {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"Gateway %q listener %q terminates %s but declares no tls.certificateRefs, "+
					"so it has no certificate to present and every handshake on port %d fails",
				object.K8sObject.GetName(), listener.Name, listener.Protocol, listener.Port),
		})
	}
	return diagnostics
}

func terminatesTLS(listener gatewayapi.GatewayListener) bool {
	switch listener.Protocol {
	case gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
	default:
		return false
	}
	if listener.TLSMode() != gatewayv1.TLSModeTerminate {
		return false
	}
	if listener.TLS == nil {
		return true
	}
	// Options are an implementation's own escape hatch for supplying
	// certificates, and Gateway API accepts them in place of certificateRefs.
	return len(listener.TLS.CertificateRefs) == 0 && len(listener.TLS.Options) == 0
}
