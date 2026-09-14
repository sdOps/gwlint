// Package danglinggatewayclass flags a Gateway whose spec.gatewayClassName
// names a GatewayClass that is not present. The field is a free-form string,
// so a typo or a class that was never installed passes schema validation and
// applies cleanly. No controller then claims the Gateway: no data plane is
// provisioned, no address is assigned, and every route attached to it stays
// unprogrammed. The only signal is an Accepted=False condition on a Gateway
// nobody looks at.
//
// GatewayClass is cluster scoped and is normally installed with the controller
// itself, in a different chart from the Gateways that name it, so the check
// judges a class name only once the manifest set contains at least one
// GatewayClass. Absent that, the set says nothing about which classes the
// cluster has, and flagging every Gateway in it would be noise.
package danglinggatewayclass

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

const templateKey = "dangling-gateway-class"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling Gateway gatewayClassName",
		Key:       templateKey,
		Description: "Flag Gateways whose gatewayClassName names a GatewayClass that is not present, " +
			"so no controller claims the Gateway and none of its listeners are ever programmed",
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

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	gateway, ok := gatewayapi.AsGateway(object.K8sObject)
	if !ok {
		return nil
	}
	className := string(gateway.Spec.GatewayClassName)
	// An empty class name is a schema violation, which kubeconform reports far
	// better than a message about a GatewayClass that was never named.
	if className == "" {
		return nil
	}

	classes := gatewayapi.GatewayClasses(lintCtx)
	if len(classes) == 0 {
		return nil
	}
	if _, found := classes[className]; found {
		return nil
	}

	return []diagnostic.Diagnostic{{
		Message: fmt.Sprintf(
			"Gateway %q names GatewayClass %q, which is not present; "+
				"no controller will claim the Gateway and none of its listeners will be programmed",
			gateway.Name, className),
	}}
}
