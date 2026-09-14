package gatewayapi

import (
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

// RouteKindsFor returns the route kinds a listener accepts when it sets no
// allowedRoutes.kinds of its own. Gateway API derives the default from the
// listener protocol.
func RouteKindsFor(protocol gatewayv1.ProtocolType) []string {
	switch protocol {
	case gatewayv1.HTTPProtocolType, gatewayv1.HTTPSProtocolType:
		return []string{gwobjectkinds.HTTPRoute, gwobjectkinds.GRPCRoute}
	case gatewayv1.TLSProtocolType:
		return []string{gwobjectkinds.TLSRoute}
	case gatewayv1.TCPProtocolType:
		return []string{gwobjectkinds.TCPRoute}
	case gatewayv1.UDPProtocolType:
		return []string{gwobjectkinds.UDPRoute}
	default:
		return nil
	}
}

// HasExplicitKinds reports whether the listener sets its own
// allowedRoutes.kinds, rather than accepting whatever its protocol defaults
// to. route-not-permitted and protocol-mismatch split on this: a kind refused
// by protocol default is protocol-mismatch's finding, and a kind refused by an
// explicit, narrower allowedRoutes.kinds is this check's, so a mismatch is
// never reported by both.
func (l Listener) HasExplicitKinds() bool {
	return l.Allowed != nil && len(l.Allowed.Kinds) > 0
}

// AcceptsKind reports whether the listener accepts the given route kind, and
// what it does accept. An explicit allowedRoutes.kinds replaces the
// protocol-derived default entirely.
func (l Listener) AcceptsKind(kind string) (bool, []string) {
	accepted := RouteKindsFor(l.Protocol)
	if l.Allowed != nil && len(l.Allowed.Kinds) > 0 {
		accepted = nil
		for _, k := range l.Allowed.Kinds {
			if k.Group != nil && string(*k.Group) != gatewayv1.GroupName {
				continue
			}
			accepted = append(accepted, string(k.Kind))
		}
	}
	// A protocol gwlint does not model accepts anything, rather than nothing.
	if accepted == nil {
		return true, nil
	}
	for _, k := range accepted {
		if k == kind {
			return true, accepted
		}
	}
	return false, accepted
}

// AcceptsNamespace reports whether the listener admits routes from the given
// namespace. "Same" is the default and means the parent's own namespace; a
// label Selector is not resolved, since deciding it needs Namespace objects a
// rendered manifest set rarely carries, so it is treated as permitting.
func (l Listener) AcceptsNamespace(routeNamespace string) bool {
	if l.Allowed == nil || l.Allowed.Namespaces == nil || l.Allowed.Namespaces.From == nil {
		return routeNamespace == l.Owner.Namespace
	}
	switch *l.Allowed.Namespaces.From {
	case gatewayv1.NamespacesFromAll:
		return true
	case gatewayv1.NamespacesFromSelector:
		return true
	case gatewayv1.NamespacesFromSame:
		return routeNamespace == l.Owner.Namespace
	default:
		return true
	}
}

// NamespacePolicy describes the listener's namespace admission, for a message.
func (l Listener) NamespacePolicy() string {
	if l.Allowed == nil || l.Allowed.Namespaces == nil || l.Allowed.Namespaces.From == nil {
		return string(gatewayv1.NamespacesFromSame)
	}
	return string(*l.Allowed.Namespaces.From)
}

// HostnamesIntersect reports whether a route carrying these hostnames can ever
// match traffic on this listener. Either side leaving hostnames unset matches
// everything; otherwise at least one pair has to overlap, where a wildcard
// covers exactly one label.
func (l Listener) HostnamesIntersect(routeHostnames []gatewayv1.Hostname) bool {
	if l.Hostname == nil || *l.Hostname == "" || len(routeHostnames) == 0 {
		return true
	}
	for _, h := range routeHostnames {
		if hostnamesOverlap(string(*l.Hostname), string(h)) {
			return true
		}
	}
	return false
}

func hostnamesOverlap(a, b string) bool {
	aWild, bWild := isWildcard(a), isWildcard(b)
	switch {
	case aWild && bWild:
		// Two wildcards overlap when either suffix contains the other.
		return hasDNSSuffix(a[1:], b[1:]) || hasDNSSuffix(b[1:], a[1:])
	case aWild:
		return wildcardMatches(a, b)
	case bWild:
		return wildcardMatches(b, a)
	default:
		return a == b
	}
}

func isWildcard(h string) bool {
	return len(h) > 2 && h[0] == '*' && h[1] == '.'
}

// wildcardMatches reports whether "*.example.com" covers a concrete hostname.
// Gateway API's wildcard stands for one or more leading labels.
func wildcardMatches(wildcard, host string) bool {
	suffix := wildcard[1:] // ".example.com"
	return len(host) > len(suffix) && hasDNSSuffix(host, suffix)
}

func hasDNSSuffix(host, suffix string) bool {
	return len(host) >= len(suffix) && host[len(host)-len(suffix):] == suffix
}
