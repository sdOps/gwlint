package missingreferencegrant

import (
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1a2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// reference is a cross-namespace backendRef reduced to the four facts a
// ReferenceGrant is matched on.
type reference struct {
	fromKind      string
	fromNamespace string
	toGroup       string
	toKind        string
	toName        string
}

// grantsByNamespace indexes every ReferenceGrant in the context by the
// namespace it lives in, which is the namespace it grants access to. A grant
// only ever authorises references into its own namespace, so nothing else about
// its location matters.
//
// ReferenceGrant is served at v1, v1beta1 and v1alpha2 at once, and v1beta1 is
// still the version most charts author. The v1beta1 and v1alpha2 types are
// defined from v1's, so their Spec is v1's Spec and needs no conversion.
func grantsByNamespace(lintCtx lintcontext.LintContext) map[string][]gatewayv1.ReferenceGrantSpec {
	byNamespace := make(map[string][]gatewayv1.ReferenceGrantSpec)
	for _, obj := range lintCtx.Objects() {
		var spec gatewayv1.ReferenceGrantSpec
		switch rg := obj.K8sObject.(type) {
		case *gatewayv1.ReferenceGrant:
			spec = rg.Spec
		case *gatewayv1b1.ReferenceGrant:
			spec = rg.Spec
		case *gatewayv1a2.ReferenceGrant:
			spec = rg.Spec
		default:
			continue
		}
		ns := obj.K8sObject.GetNamespace()
		byNamespace[ns] = append(byNamespace[ns], spec)
	}
	return byNamespace
}

// permits reports whether any of the grants authorises ref. Gateway API
// combines both the from and the to lists with OR, and a grant permits a
// reference only when one entry on each side matches.
func permits(grants []gatewayv1.ReferenceGrantSpec, ref reference) bool {
	for _, spec := range grants {
		if matchesFrom(spec.From, ref) && matchesTo(spec.To, ref) {
			return true
		}
	}
	return false
}

// matchesFrom matches the referring side. Only routes reach this check, so the
// group is always Gateway API's; an empty group in the grant means core and so
// never matches a route.
func matchesFrom(from []gatewayv1.ReferenceGrantFrom, ref reference) bool {
	for _, f := range from {
		if string(f.Group) == gatewayapiGroup &&
			string(f.Kind) == ref.fromKind &&
			string(f.Namespace) == ref.fromNamespace {
			return true
		}
	}
	return false
}

// matchesTo matches the referred-to side. An omitted name covers every object
// of that group and kind in the grant's namespace, which is how most charts
// write it.
func matchesTo(to []gatewayv1.ReferenceGrantTo, ref reference) bool {
	for _, t := range to {
		if string(t.Group) != ref.toGroup || string(t.Kind) != ref.toKind {
			continue
		}
		if t.Name == nil || string(*t.Name) == ref.toName {
			return true
		}
	}
	return false
}
