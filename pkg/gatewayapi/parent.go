package gatewayapi

import (
	egv1a1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	corev1 "k8s.io/api/core/v1"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// Parent is a resolved parentRef: what a route says it attaches to, with
// Gateway API's defaults already applied.
type Parent struct {
	Group     string
	Kind      string
	Namespace string
	Name      string
	// Section is the listener the route attaches to, or nil for every listener
	// on the parent.
	Section *gatewayv1.SectionName
}

// ParentsOf resolves a route's parentRefs. An omitted kind means Gateway and an
// omitted namespace means the route's own, both per Gateway API.
func ParentsOf(route Route) []Parent {
	parents := make([]Parent, 0, len(route.ParentRefs))
	for _, ref := range route.ParentRefs {
		p := Parent{
			Group:     gatewayv1.GroupName,
			Kind:      gwobjectkinds.Gateway,
			Namespace: route.Namespace,
			Name:      string(ref.Name),
			Section:   ref.SectionName,
		}
		if ref.Group != nil {
			p.Group = string(*ref.Group)
		}
		if ref.Kind != nil {
			p.Kind = string(*ref.Kind)
		}
		if ref.Namespace != nil {
			p.Namespace = string(*ref.Namespace)
		}
		parents = append(parents, p)
	}
	return parents
}

// Matches reports whether the parent names the given object.
func (p Parent) Matches(kind, namespace, name string) bool {
	return p.Group == gatewayv1.GroupName && p.Kind == kind && p.Namespace == namespace && p.Name == name
}

// CoversListener reports whether a policy scoped to the given listener covers a
// route attached through this parentRef. A parentRef naming no listener
// attaches the route to every listener, so it stays covered.
func (p Parent) CoversListener(listener *gatewayv1.SectionName) bool {
	return listener == nil || p.Section == nil || *p.Section == *listener
}

// ObjectRef identifies an object by kind, namespace and name.
type ObjectRef struct {
	Kind      string
	Namespace string
	Name      string
}

// GatewaysAndListenerSets indexes every Gateway and ListenerSet in the context,
// which is how a check decides whether a parentRef resolves to anything.
func GatewaysAndListenerSets(lintCtx lintcontext.LintContext) map[ObjectRef]struct{} {
	found := make(map[ObjectRef]struct{})
	for _, obj := range lintCtx.Objects() {
		var kind string
		switch obj.K8sObject.(type) {
		case *gatewayv1.Gateway:
			kind = gwobjectkinds.Gateway
		case *gatewayv1.ListenerSet:
			kind = gwobjectkinds.ListenerSet
		default:
			continue
		}
		found[ObjectRef{Kind: kind, Namespace: obj.K8sObject.GetNamespace(), Name: obj.K8sObject.GetName()}] = struct{}{}
	}
	return found
}

// ListenerSetParent returns the Gateway a ListenerSet attaches to.
func ListenerSetParent(ls *gatewayv1.ListenerSet) ObjectRef {
	parent := ls.Spec.ParentRef
	ns := ls.Namespace
	if parent.Namespace != nil {
		ns = string(*parent.Namespace)
	}
	return ObjectRef{Kind: gwobjectkinds.Gateway, Namespace: ns, Name: string(parent.Name)}
}

// GroupName is the Gateway API group, re-exported so checks do not each import
// the API package just to compare a group string.
const GroupName = gatewayv1.GroupName

// BackendTargets indexes the objects a backendRef can resolve to: core
// Services and Envoy Gateway Backends. Kinds gwlint has no typed knowledge of
// are absent, so a check can tell "not there" from "cannot judge".
func BackendTargets(lintCtx lintcontext.LintContext) map[ObjectRef]struct{} {
	found := make(map[ObjectRef]struct{})
	for _, obj := range lintCtx.Objects() {
		var kind string
		switch obj.K8sObject.(type) {
		case *corev1.Service:
			kind = "Service"
		case *egv1a1.Backend:
			kind = egv1a1.KindBackend
		default:
			continue
		}
		found[ObjectRef{Kind: kind, Namespace: obj.K8sObject.GetNamespace(), Name: obj.K8sObject.GetName()}] = struct{}{}
	}
	return found
}

// ResolvableBackendKind reports whether gwlint can judge a backendRef of this
// group and kind at all. A ref into some other vendor's CRD is not something
// the absence of an object says anything about.
func ResolvableBackendKind(group, kind string) bool {
	switch {
	case group == "" && kind == "Service":
		return true
	case group == egv1a1.GroupName && kind == egv1a1.KindBackend:
		return true
	default:
		return false
	}
}
