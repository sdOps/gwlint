// Package hostnameonnonhostnameprotocol flags a Gateway listener that sets a
// hostname on TCP or UDP. Neither protocol carries a hostname anywhere a
// Gateway could read one: there is no Host header and no SNI, so Gateway API
// ignores the field. The listener goes on admitting every connection to its
// port, which is the opposite of what somebody writing a hostname there meant.
//
// The CRD forbids this with a CEL rule, and the apiserver enforces it, but the
// validators people run before an apply do not: kubeconform and its kin check
// structure against the OpenAPI schema and never evaluate
// x-kubernetes-validations. So the manifest passes review, passes CI, and the
// hostname quietly means nothing.
//
// HTTPS and TLS listeners match on Host and SNI respectively, and HTTP on the
// Host header, so a hostname on any of those is meaningful and left alone.
//
// The check reads a single Gateway and resolves no references, so a partial
// manifest set needs no guard: the finding depends on nothing outside the
// object.
package hostnameonnonhostnameprotocol

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

const templateKey = "hostname-on-non-hostname-protocol"

func init() {
	templates.Register(check.Template{
		HumanName: "Listener hostname on a protocol without hostnames",
		Key:       templateKey,
		Description: "Flag Gateway listeners that set a hostname on TCP or UDP, where Gateway API " +
			"ignores it and the listener keeps admitting every connection on its port",
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
		switch listener.Protocol {
		case gatewayv1.TCPProtocolType, gatewayv1.UDPProtocolType:
		default:
			continue
		}
		// An empty hostname is how the CRD's own rule spells "unset", so treat
		// the two the same rather than flagging a field that says nothing.
		if listener.Hostname == nil || *listener.Hostname == "" {
			continue
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{
			Message: fmt.Sprintf(
				"Gateway %q listener %q sets hostname %q on protocol %s, which has neither SNI nor a "+
					"Host header to match it; Gateway API ignores the hostname and the listener still "+
					"admits every connection on port %d",
				object.K8sObject.GetName(), listener.Name, *listener.Hostname, listener.Protocol, listener.Port),
		})
	}
	return diagnostics
}
