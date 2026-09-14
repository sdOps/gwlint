// Package conflictingroutematch flags two HTTPRoutes that claim the same
// hostname and the same request match on the same listener, where nothing
// decides which of them wins. Both apply cleanly, both report Accepted, and
// the implementation then serves one of them; which one is not something the
// manifests say. Traffic lands on whichever route the controller happened to
// pick, and a later edit can silently swap it.
//
// Gateway API resolves most overlaps on its own, so most of them are not
// findings: a longer path prefix beats a shorter one, an exact hostname beats
// a wildcard, more header or query parameter matches beat fewer, and an older
// creationTimestamp breaks what is left. This check only reports the residue,
// where two routes carry the byte-identical match on the same hostname and the
// same listener and no timestamp separates them. A schema validator sees two
// perfectly valid HTTPRoutes, because the conflict exists only between them.
//
// Guard for partial manifest sets: the check reasons about the listener two
// routes share, so it only judges a parentRef whose Gateway or ListenerSet is
// actually present in the set, with its listeners. Linting routes without the
// chart that defines the Gateway is normal, and guessing that two routes share
// a listener that nothing in the set describes would flag routes that attach to
// different listeners or that a listener's allowedRoutes never admits.
package conflictingroutematch

import (
	"fmt"
	"sort"

	"golang.stackrox.io/kube-linter/pkg/check"
	"golang.stackrox.io/kube-linter/pkg/config"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext"
	"golang.stackrox.io/kube-linter/pkg/templates"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/sdOps/gwlint/pkg/gatewayapi"
	gwobjectkinds "github.com/sdOps/gwlint/pkg/objectkinds"
)

const templateKey = "conflicting-route-match"

func init() {
	templates.Register(check.Template{
		HumanName: "Conflicting HTTPRoute match",
		Key:       templateKey,
		Description: "Flag two HTTPRoutes that claim the same hostname with an identical request match on the same listener, " +
			"where Gateway API's precedence rules leave the winner undefined",
		SupportedObjectKinds: config.ObjectKindsDesc{
			ObjectKinds: []string{gwobjectkinds.HTTPRoute},
		},
		ParseAndValidateParams: func(_ map[string]interface{}) (interface{}, error) {
			return nil, nil
		},
		Instantiate: func(_ interface{}) (check.Func, error) {
			return checkFunc, nil
		},
	})
}

// candidate is an HTTPRoute reduced to what deciding a conflict needs.
type candidate struct {
	id        gatewayapi.ObjectRef
	route     gatewayapi.Route
	matches   []gatewayapi.HTTPMatch
	createdAt int64
	// hasCreated records whether the manifest carried a creationTimestamp at
	// all. Rendered chart output almost never does; output read back from a
	// cluster always does.
	hasCreated bool
}

// attachment is one listener a route attaches through.
type attachment struct {
	owner    gatewayapi.ObjectRef
	listener gatewayv1.SectionName
}

func checkFunc(lintCtx lintcontext.LintContext, object lintcontext.Object) []diagnostic.Diagnostic {
	self, ok := asCandidate(object)
	if !ok {
		return nil
	}

	listenersByOwner := gatewayapi.ListenersByOwner(lintCtx)
	mine := attachmentsOf(self.route, listenersByOwner)
	if len(mine) == 0 {
		return nil
	}

	var diagnostics []diagnostic.Diagnostic
	for _, obj := range lintCtx.Objects() {
		other, ok := asCandidate(obj)
		if !ok || other.id == self.id {
			continue
		}
		// The conflict belongs to the pair, not to either route, so report it
		// once from the side that sorts later rather than twice. Sorting by
		// namespace and name is also Gateway API's own last-resort tiebreak,
		// which makes the reported route the one that loses it.
		if !sortsBefore(other.id, self.id) {
			continue
		}
		if separatedByAge(self, other) {
			continue
		}
		if d, found := conflict(self, other, mine, listenersByOwner); found {
			diagnostics = append(diagnostics, d)
		}
	}
	return diagnostics
}

func asCandidate(object lintcontext.Object) (candidate, bool) {
	route, ok := gatewayapi.AsRoute(object.K8sObject)
	if !ok {
		return candidate{}, false
	}
	matches, ok := gatewayapi.HTTPMatchesOf(object.K8sObject)
	if !ok || len(matches) == 0 {
		return candidate{}, false
	}
	created := object.K8sObject.GetCreationTimestamp()
	return candidate{
		id: gatewayapi.ObjectRef{
			Kind: string(route.Kind), Namespace: route.Namespace, Name: string(route.Name),
		},
		route:      route,
		matches:    matches,
		createdAt:  created.Unix(),
		hasCreated: !created.IsZero(),
	}, true
}

// separatedByAge reports whether Gateway API's creationTimestamp tiebreak
// already decides the pair. It only does so when both routes state a time and
// the times differ.
func separatedByAge(a, b candidate) bool {
	return a.hasCreated && b.hasCreated && a.createdAt != b.createdAt
}

func sortsBefore(a, b gatewayapi.ObjectRef) bool {
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	return a.Name < b.Name
}

// attachmentsOf returns the listeners the route actually attaches through,
// keyed so two routes' sets can be intersected. A parentRef naming no listener
// attaches to every listener on the parent, which is why the parent's listeners
// have to be read rather than the sectionName compared.
func attachmentsOf(route gatewayapi.Route, listenersByOwner map[gatewayapi.ObjectRef][]gatewayapi.Listener) map[attachment]gatewayapi.Listener {
	found := make(map[attachment]gatewayapi.Listener)
	for _, parent := range gatewayapi.ParentsOf(route) {
		if parent.Group != gatewayapi.GroupName {
			continue
		}
		owner := gatewayapi.ObjectRef{Kind: parent.Kind, Namespace: parent.Namespace, Name: parent.Name}
		for _, listener := range listenersByOwner[owner] {
			// A parentRef naming no listener attaches the route to every
			// listener on the parent.
			if parent.Section != nil && *parent.Section != listener.Name {
				continue
			}
			// A listener that would never admit the route is not a listener
			// the route contends on.
			if accepts, _ := listener.AcceptsKind(gwobjectkinds.HTTPRoute); !accepts {
				continue
			}
			if !listener.AcceptsNamespace(route.Namespace) {
				continue
			}
			found[attachment{owner: owner, listener: listener.Name}] = listener
		}
	}
	return found
}

// conflict returns the diagnostic for the first shared listener, hostname and
// match the two routes contend on, in a deterministic order so the message does
// not move between runs.
func conflict(self, other candidate, mine map[attachment]gatewayapi.Listener,
	listenersByOwner map[gatewayapi.ObjectRef][]gatewayapi.Listener) (diagnostic.Diagnostic, bool) {
	theirs := attachmentsOf(other.route, listenersByOwner)
	shared := make([]attachment, 0, len(mine))
	for at := range mine {
		if _, both := theirs[at]; both {
			shared = append(shared, at)
		}
	}
	if len(shared) == 0 {
		return diagnostic.Diagnostic{}, false
	}
	sort.Slice(shared, func(i, j int) bool { return attachmentLess(shared[i], shared[j]) })

	hostnames := contestedHostnames(self.route.Hostnames, other.route.Hostnames)
	if len(hostnames) == 0 {
		return diagnostic.Diagnostic{}, false
	}
	match, found := identicalMatch(self.matches, other.matches)
	if !found {
		return diagnostic.Diagnostic{}, false
	}

	for _, at := range shared {
		listener := mine[at]
		for _, hostname := range hostnames {
			// A hostname the listener itself will not serve is not contested
			// on that listener, whatever the two routes say.
			if hostname != "" && !listener.HostnamesIntersect([]gatewayv1.Hostname{gatewayv1.Hostname(hostname)}) {
				continue
			}
			claimed := fmt.Sprintf("hostname %q", hostname)
			if hostname == "" {
				claimed = "every hostname the listener serves"
			}
			return diagnostic.Diagnostic{
				Message: fmt.Sprintf(
					"HTTPRoute %q and HTTPRoute %q in namespace %q both claim %s with the identical match %s "+
						"on listener %q of %s %q in namespace %q; Gateway API's precedence rules leave the tie "+
						"unbroken, so which route serves a matching request is undefined",
					self.id.Name, other.id.Name, other.id.Namespace, claimed, match.Describe,
					at.listener, at.owner.Kind, at.owner.Name, at.owner.Namespace),
			}, true
		}
	}
	return diagnostic.Diagnostic{}, false
}

func attachmentLess(a, b attachment) bool {
	if a.owner != b.owner {
		if a.owner.Namespace != b.owner.Namespace {
			return a.owner.Namespace < b.owner.Namespace
		}
		if a.owner.Name != b.owner.Name {
			return a.owner.Name < b.owner.Name
		}
		return a.owner.Kind < b.owner.Kind
	}
	return a.listener < b.listener
}

// contestedHostnames returns the hostnames the two routes claim with equal
// specificity, sorted. Gateway API ranks a longer hostname match ahead of a
// shorter one, so a wildcard against an exact name, or a named host against a
// route that names none, is already decided and contributes nothing. Two routes
// that both name no hostname contend over whatever the listener serves, which
// the empty string stands for.
func contestedHostnames(mine, theirs []gatewayv1.Hostname) []string {
	if len(mine) == 0 && len(theirs) == 0 {
		return []string{""}
	}
	if len(mine) == 0 || len(theirs) == 0 {
		return nil
	}
	seen := make(map[gatewayv1.Hostname]struct{}, len(theirs))
	for _, h := range theirs {
		seen[h] = struct{}{}
	}
	var contested []string
	for _, h := range mine {
		if _, both := seen[h]; both {
			contested = append(contested, string(h))
		}
	}
	sort.Strings(contested)
	return contested
}

// identicalMatch returns a match the two routes spell identically. Anything
// short of identical leaves Gateway API a specificity comparison to make, which
// is a decision the manifests do state.
func identicalMatch(mine, theirs []gatewayapi.HTTPMatch) (gatewayapi.HTTPMatch, bool) {
	seen := make(map[string]struct{}, len(theirs))
	for _, m := range theirs {
		seen[m.Key] = struct{}{}
	}
	var best gatewayapi.HTTPMatch
	found := false
	for _, m := range mine {
		if _, both := seen[m.Key]; !both {
			continue
		}
		if !found || m.Key < best.Key {
			best, found = m, true
		}
	}
	return best, found
}
