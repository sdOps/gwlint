package gatewayapi

import (
	"golang.stackrox.io/kube-linter/pkg/k8sutil"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1b1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// GatewayListener is one entry of a Gateway's spec.listeners, kept with its
// tls block. Listener models what a route has to agree with for traffic to
// flow and drops tls; whether a listener can serve what its protocol promises
// is decided inside that block, so checks that judge a listener on its own
// terms need it.
type GatewayListener struct {
	Name     gatewayv1.SectionName
	Hostname *gatewayv1.Hostname
	Port     gatewayv1.PortNumber
	Protocol gatewayv1.ProtocolType
	TLS      *gatewayv1.ListenerTLSConfig
}

// GatewayListenersOf returns the listeners obj declares if it is a Gateway at
// any served version, and reports whether it was one. v1beta1's Gateway is a
// defined type over v1's rather than a separate struct, so it converts
// directly; without that case a Gateway authored as v1beta1 would decode into
// a type no check looks at.
func GatewayListenersOf(obj k8sutil.Object) ([]GatewayListener, bool) {
	var gw *gatewayv1.Gateway
	switch typed := obj.(type) {
	case *gatewayv1.Gateway:
		gw = typed
	case *gatewayv1b1.Gateway:
		gw = (*gatewayv1.Gateway)(typed)
	default:
		return nil, false
	}

	listeners := make([]GatewayListener, 0, len(gw.Spec.Listeners))
	for _, l := range gw.Spec.Listeners {
		listeners = append(listeners, GatewayListener{
			Name:     l.Name,
			Hostname: l.Hostname,
			Port:     l.Port,
			Protocol: l.Protocol,
			TLS:      l.TLS,
		})
	}
	return listeners, true
}

// TLSMode returns the listener's TLS mode with Gateway API's default applied.
// An unset mode means Terminate, so a listener that says nothing about TLS is
// still expected to hold a certificate.
func (l GatewayListener) TLSMode() gatewayv1.TLSModeType {
	if l.TLS == nil || l.TLS.Mode == nil || *l.TLS.Mode == "" {
		return gatewayv1.TLSModeTerminate
	}
	return *l.TLS.Mode
}
