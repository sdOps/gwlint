package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// GatewayClasses indexes every GatewayClass in the context by name. A
// GatewayClass is cluster scoped, so a name on its own identifies one and
// there is no namespace to key it by. v1beta1's GatewayClass is a distinct Go
// type over v1's rather than an alias, so both served versions are matched.
func GatewayClasses(lintCtx lintcontext.LintContext) map[string]struct{} {
	found := make(map[string]struct{})
	for _, obj := range lintCtx.Objects() {
		switch obj.K8sObject.(type) {
		case *gatewayv1.GatewayClass, *gatewayv1b1.GatewayClass:
			found[obj.K8sObject.GetName()] = struct{}{}
		}
	}
	return found
}
