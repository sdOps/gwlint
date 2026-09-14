// Package conflictinglisteners flags listeners on one Gateway or ListenerSet
// that share a port and cannot both be programmed. Gateway API calls such
// listeners indistinct, and an implementation must not pick a winner: every
// listener in the conflicting group is refused, so a port that looks
// configured serves nothing. The reason lands in a Conflicted listener status
// condition, which nobody reads.
//
// Two shapes are flagged, both taken from the upstream Gateway CRD's own
// validation rules:
//
//   - Same port, same protocol, same hostname, which the upstream CEL rule
//     "Combination of port, protocol and hostname must be unique for each
//     listener" forbids. For TCP and UDP no hostname exists, so the port and
//     protocol alone have to differ.
//   - Same port, protocols that cannot share one. A raw TCP listener makes
//     every HTTP, HTTPS or TLS listener on its port indistinct, and a
//     plaintext HTTP listener cannot share a port with a TLS one.
//
// A schema validator misses both: `x-kubernetes-validations` is a Kubernetes
// extension that kubeconform and the schema checks in helm and kustomize do
// not evaluate, and the protocol rules are prose in the spec rather than CRD
// validation at all.
//
// Deliberately not flagged: HTTPS alongside TLS on one port. The spec calls
// that distinct, and whether an implementation can serve it is Extended
// support that varies, so flagging it would be a guess. Listeners are also
// compared only within the object that declares them; a ListenerSet's
// listeners merge into its parent Gateway's set at runtime, and judging that
// merged set is a separate problem from judging one object's own spec.
//
// The check resolves nothing across objects, so it needs no
// partial-manifest-set guard: a Gateway linted on its own carries everything
// the check reads, and no absent object can make it fire.
package conflictinglisteners

import (
	"fmt"
	"sort"
	"strings"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "conflicting-listeners"

func init() {
	templates.Register(check.Template{
		HumanName: "Conflicting Listeners",
		Key:       templateKey,
		Description: "Flag listeners sharing a port that Gateway API cannot tell apart, either because " +
			"their protocols cannot share a port or because port, protocol and hostname are identical",
		SupportedObjectKinds: config.ObjectKindsDesc{
			// A ListenerSet declares listeners under the same rules, down to
			// the same CEL validation on the field.
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

func checkFunc(_ lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	listeners := gatewayapi.ListenersOf(object.K8sObject)
	if len(listeners) < 2 {
		return nil
	}
	owner := listeners[0]

	byPort := make(map[gatewayv1.PortNumber][]gatewayapi.Listener, len(listeners))
	for _, l := range listeners {
		byPort[l.Port] = append(byPort[l.Port], l)
	}
	ports := make([]gatewayv1.PortNumber, 0, len(byPort))
	for port := range byPort {
		ports = append(ports, port)
	}
	sort.Slice(ports, func(i, j int) bool { return ports[i] < ports[j] })

	var diagnostics []diagnostic.Diagnostic
	for _, port := range ports {
		group := byPort[port]
		if len(group) < 2 {
			continue
		}
		diagnostics = append(diagnostics, indistinctListeners(owner, port, group)...)
		diagnostics = append(diagnostics, protocolConflicts(owner, port, group)...)
	}
	return diagnostics
}

// listenerKey is what has to be unique for a listener to be addressable:
// protocol plus hostname, within a port. An unset and an empty hostname are
// the same catch-all, which is why they collapse to one key.
type listenerKey struct {
	protocol gatewayv1.ProtocolType
	hostname string
}

func keyOf(l gatewayapi.Listener) listenerKey {
	key := listenerKey{protocol: l.Protocol}
	if l.Hostname != nil {
		key.hostname = string(*l.Hostname)
	}
	return key
}

// indistinctListeners reports listeners on one port that agree on protocol and
// hostname, so nothing in an inbound request can steer traffic to one of them.
func indistinctListeners(owner gatewayapi.Listener, port gatewayv1.PortNumber, group []gatewayapi.Listener) []diagnostic.Diagnostic {
	byKey := make(map[listenerKey][]gatewayapi.Listener, len(group))
	for _, l := range group {
		byKey[keyOf(l)] = append(byKey[keyOf(l)], l)
	}
	keys := make([]listenerKey, 0, len(byKey))
	for key, ls := range byKey {
		if len(ls) > 1 {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].protocol != keys[j].protocol {
			return keys[i].protocol < keys[j].protocol
		}
		return keys[i].hostname < keys[j].hostname
	})

	diagnostics := make([]diagnostic.Diagnostic, 0, len(keys))
	for _, key := range keys {
		var message string
		// A protocol gwlint does not model may still carry a hostname, so what
		// decides the wording is whether one is set, not the protocol alone.
		if key.hostname != "" || usesHostname(key.protocol) {
			message = fmt.Sprintf(
				"%s %q has %s on port %d, each with protocol %s and %s; port, protocol and hostname together "+
					"must identify one listener, so none of them is distinct and none is accepted",
				owner.OwnerKind, owner.Owner.Name, listenerNames(byKey[key]), port, key.protocol,
				describeHostname(key.hostname))
		} else {
			message = fmt.Sprintf(
				"%s %q has %s on port %d, each with protocol %s, which carries no hostname to tell them apart; "+
					"none of them is distinct and none is accepted",
				owner.OwnerKind, owner.Owner.Name, listenerNames(byKey[key]), port, key.protocol)
		}
		diagnostics = append(diagnostics, diagnostic.Diagnostic{Message: message})
	}
	return diagnostics
}

// protocolConflicts reports pairs of protocols on one port that no
// implementation can serve together, whatever their hostnames say.
func protocolConflicts(owner gatewayapi.Listener, port gatewayv1.PortNumber, group []gatewayapi.Listener) []diagnostic.Diagnostic {
	byProtocol := make(map[gatewayv1.ProtocolType][]gatewayapi.Listener, len(group))
	for _, l := range group {
		byProtocol[l.Protocol] = append(byProtocol[l.Protocol], l)
	}
	protocols := make([]string, 0, len(byProtocol))
	for p := range byProtocol {
		protocols = append(protocols, string(p))
	}
	sort.Strings(protocols)

	var diagnostics []diagnostic.Diagnostic
	for i := range protocols {
		for j := i + 1; j < len(protocols); j++ {
			a, b := gatewayv1.ProtocolType(protocols[i]), gatewayv1.ProtocolType(protocols[j])
			reason, conflict := protocolsConflict(a, b)
			if !conflict {
				continue
			}
			diagnostics = append(diagnostics, diagnostic.Diagnostic{
				Message: fmt.Sprintf(
					"%s %q has %s with protocol %s and %s with protocol %s on port %d; %s",
					owner.OwnerKind, owner.Owner.Name,
					listenerNames(byProtocol[a]), a, listenerNames(byProtocol[b]), b, port, reason),
			})
		}
	}
	return diagnostics
}

// protocolClass groups protocols by what sharing a port with them means.
type protocolClass int

const (
	// classUnknown covers anything gwlint has no rule for, including the
	// custom protocol values Gateway API lets an implementation define.
	classUnknown protocolClass = iota
	classPlaintext
	classTLS
	classRawTCP
	classUDP
)

func classOf(p gatewayv1.ProtocolType) protocolClass {
	switch p {
	case gatewayv1.HTTPProtocolType:
		return classPlaintext
	case gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
		return classTLS
	case gatewayv1.TCPProtocolType:
		return classRawTCP
	case gatewayv1.UDPProtocolType:
		return classUDP
	default:
		return classUnknown
	}
}

func usesHostname(p gatewayv1.ProtocolType) bool {
	switch p {
	case gatewayv1.HTTPProtocolType, gatewayv1.HTTPSProtocolType, gatewayv1.TLSProtocolType:
		return true
	default:
		return false
	}
}

// protocolsConflict reports whether two different protocols can share a port,
// and why not. UDP never conflicts with the TCP-based protocols, since a UDP
// port and a TCP port of the same number are different sockets: DNS on 53 over
// both is a normal thing to configure.
func protocolsConflict(a, b gatewayv1.ProtocolType) (string, bool) {
	classA, classB := classOf(a), classOf(b)
	if classA == classUnknown || classB == classUnknown {
		return "", false
	}
	if (classA == classUDP) != (classB == classUDP) {
		return "", false
	}
	switch {
	case classA == classRawTCP || classB == classRawTCP:
		return "a listener sharing a port with a raw TCP listener cannot be told apart from it, " +
			"so neither of them is accepted and the port serves nothing", true
	case classA == classPlaintext || classB == classPlaintext:
		return "one port cannot carry both plaintext and TLS traffic, " +
			"so neither listener is accepted and the port serves nothing", true
	default:
		// Two TLS-based protocols, which the spec calls distinct.
		return "", false
	}
}

func describeHostname(hostname string) string {
	if hostname == "" {
		return "no hostname, so they match every request"
	}
	return fmt.Sprintf("hostname %q", hostname)
}

// listenerNames names the listeners a finding is about, so the reader can go
// straight to them without counting entries in the spec.
func listenerNames(listeners []gatewayapi.Listener) string {
	names := make([]string, 0, len(listeners))
	for _, l := range listeners {
		names = append(names, fmt.Sprintf("%q", l.Name))
	}
	sort.Strings(names)
	if len(names) == 1 {
		return "listener " + names[0]
	}
	return "listeners " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}
