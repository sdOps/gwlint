// Package danglinglistenersetparent flags a ListenerSet whose spec.parentRef
// names a Gateway that is not present. A ListenerSet carries no listeners of
// its own on the wire: its entries are merged into the parent Gateway's
// listener list. Name a Gateway that does not exist and the apply still
// succeeds, but nothing is ever bound, and any route that attaches through
// that ListenerSet goes unprogrammed with it. Two objects away from the route,
// which is what makes it hard to spot by reading manifests.
//
// Gateways routinely live in a different chart from the ListenerSets that
// extend them, so the check judges a parentRef only once the manifest set
// contains at least one Gateway in the namespace the ref resolves to. A set
// that says nothing about that namespace is not evidence the Gateway is
// missing.
package danglinglistenersetparent

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

const templateKey = "dangling-listener-set-parent"

func init() {
	templates.Register(check.Template{
		HumanName: "Dangling ListenerSet parentRef",
		Key:       templateKey,
		Description: "Flag ListenerSets whose parentRef names a Gateway that is not present, " +
			"so their listeners are never merged into a Gateway and never bound",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.ListenerSet},
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
	listenerSet, ok := object.K8sObject.(*gatewayv1.ListenerSet)
	if !ok {
		return nil
	}
	ref := listenerSet.Spec.ParentRef
	// A parentRef into another API group, or at some kind other than Gateway,
	// belongs to an implementation gwlint does not model, and its absence here
	// says nothing. A missing name is a schema violation, not a dangling ref.
	if ref.Group != nil && string(*ref.Group) != gatewayapi.GroupName {
		return nil
	}
	if ref.Kind != nil && string(*ref.Kind) != gwobjectkinds.Gateway {
		return nil
	}
	if ref.Name == "" {
		return nil
	}

	parent := gatewayapi.ListenerSetParent(listenerSet)
	gateways := gateways(lintCtx)
	if !describesNamespace(gateways, parent.Namespace) {
		return nil
	}
	if _, found := gateways[parent]; found {
		return nil
	}

	return []diagnostic.Diagnostic{{
		Message: fmt.Sprintf(
			"ListenerSet %q attaches to Gateway %q in namespace %q, which is not present; "+
				"its listeners will never be merged into a Gateway and will never be bound",
			listenerSet.Name, parent.Name, parent.Namespace),
	}}
}

// gateways indexes every Gateway in the context, at any served version. Only
// this check resolves a reference to a Gateway alone today, so the index stays
// here rather than in pkg/gatewayapi.
func gateways(lintCtx lintcontext.LintContext) map[gatewayapi.ObjectRef]struct{} {
	found := make(map[gatewayapi.ObjectRef]struct{})
	for _, obj := range lintCtx.Objects() {
		if _, ok := gatewayapi.AsGateway(obj.K8sObject); !ok {
			continue
		}
		found[gatewayapi.ObjectRef{
			Kind:      gwobjectkinds.Gateway,
			Namespace: obj.K8sObject.GetNamespace(),
			Name:      obj.K8sObject.GetName(),
		}] = struct{}{}
	}
	return found
}

// describesNamespace reports whether the manifest set has anything to say
// about the Gateways in a namespace. Namespace is the granularity that matters
// here: a set can carry the Gateway for one namespace while saying nothing
// about another that a ListenerSet reaches into.
func describesNamespace(gateways map[gatewayapi.ObjectRef]struct{}, namespace string) bool {
	for ref := range gateways {
		if ref.Namespace == namespace {
			return true
		}
	}
	return false
}
