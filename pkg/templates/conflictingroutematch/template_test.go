package conflictingroutematch

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"golang.stackrox.io/kube-linter/pkg/diagnostic"
	"golang.stackrox.io/kube-linter/pkg/lintcontext/mocks"
	"golang.stackrox.io/kube-linter/pkg/templates"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const (
	namespace   = "storefront"
	gatewayName = "storefront-gateway"
	hostname    = "shop.example.com"
	// alphaRoute sorts before betaRoute, so the pair is reported on betaRoute.
	alphaRoute = "alpha-checkout"
	betaRoute  = "beta-checkout"
)

func TestConflictingRouteMatch(t *testing.T) {
	suite.Run(t, new(ConflictingRouteMatchTestSuite))
}

type ConflictingRouteMatchTestSuite struct {
	templates.TemplateTestSuite

	ctx *mocks.MockLintContext
}

func (s *ConflictingRouteMatchTestSuite) SetupTest() {
	s.Init(templateKey)
	s.ctx = mocks.NewMockContext()
}

func gvk(kind string) schema.GroupVersionKind {
	return schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind(kind)
}

func sectionPtr(s gatewayv1.SectionName) *gatewayv1.SectionName { return &s }
func hostPtr(h gatewayv1.Hostname) *gatewayv1.Hostname          { return &h }
func pathTypePtr(t gatewayv1.PathMatchType) *gatewayv1.PathMatchType {
	return &t
}
func strPtr(v string) *string { return &v }

// parentRef builds a parentRef to the suite's Gateway. An empty section means
// no sectionName, which attaches the route to every listener.
func parentRef(section string) gatewayv1.ParentReference {
	ref := gatewayv1.ParentReference{Name: gatewayName}
	if section != "" {
		ref.SectionName = sectionPtr(gatewayv1.SectionName(section))
	}
	return ref
}

func pathMatch(matchType gatewayv1.PathMatchType, value string) gatewayv1.HTTPRouteMatch {
	return gatewayv1.HTTPRouteMatch{
		Path: &gatewayv1.HTTPPathMatch{Type: pathTypePtr(matchType), Value: strPtr(value)},
	}
}

func prefix(value string) gatewayv1.HTTPRouteMatch {
	return pathMatch(gatewayv1.PathMatchPathPrefix, value)
}

func hosts(names ...string) []gatewayv1.Hostname {
	out := make([]gatewayv1.Hostname, 0, len(names))
	for _, n := range names {
		out = append(out, gatewayv1.Hostname(n))
	}
	return out
}

func httpListener(name, listenerHostname string) gatewayv1.Listener {
	l := gatewayv1.Listener{
		Name:     gatewayv1.SectionName(name),
		Port:     80,
		Protocol: gatewayv1.HTTPProtocolType,
	}
	if listenerHostname != "" {
		l.Hostname = hostPtr(gatewayv1.Hostname(listenerHostname))
	}
	return l
}

func (s *ConflictingRouteMatchTestSuite) addGateway(listeners ...gatewayv1.Listener) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: gatewayName, Namespace: namespace},
		Spec:       gatewayv1.GatewaySpec{GatewayClassName: "eg", Listeners: listeners},
	}
	gw.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-"+gatewayName, gw)
}

// addRoute registers an HTTPRoute whose single rule carries the given matches.
func (s *ConflictingRouteMatchTestSuite) addRoute(
	name string, parents []gatewayv1.ParentReference, hostnames []gatewayv1.Hostname,
	matches ...gatewayv1.HTTPRouteMatch,
) {
	s.addRouteWithRules(name, parents, hostnames, gatewayv1.HTTPRouteRule{Matches: matches})
}

func (s *ConflictingRouteMatchTestSuite) addRouteWithRules(
	name string, parents []gatewayv1.ParentReference, hostnames []gatewayv1.Hostname,
	rules ...gatewayv1.HTTPRouteRule,
) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: parents},
			Hostnames:       hostnames,
			Rules:           rules,
		},
	}
	route.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(name, route)
}

// want renders the diagnostic the check produces for a pair on the suite's
// Gateway. An empty contested hostname means both routes named none.
func want(later, earlier, contested, match, listener string) string {
	claimed := fmt.Sprintf("hostname %q", contested)
	if contested == "" {
		claimed = "every hostname the listener serves"
	}
	return fmt.Sprintf(
		"HTTPRoute %q and HTTPRoute %q in namespace %q both claim %s with the identical match %s "+
			"on listener %q of %s %q in namespace %q; Gateway API's precedence rules leave the tie "+
			"unbroken, so which route serves a matching request is undefined",
		later, earlier, namespace, claimed, match, listener, "Gateway", gatewayName, namespace)
}

func (s *ConflictingRouteMatchTestSuite) expect(diagnosticsByObject map[string][]string) {
	expected := map[string][]diagnostic.Diagnostic{}
	for name, msgs := range diagnosticsByObject {
		for _, m := range msgs {
			expected[name] = append(expected[name], diagnostic.Diagnostic{Message: m})
		}
	}
	s.Validate(s.ctx, []templates.TestCase{{Diagnostics: expected}})
}

// addConflictingPair is the shape most tests start from: two routes with the
// identical match on the same listener and hostname.
func (s *ConflictingRouteMatchTestSuite) addConflictingPair() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))
}

func (s *ConflictingRouteMatchTestSuite) TestFlagsIdenticalMatchOnTheSameListener() {
	s.addConflictingPair()

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/checkout"`, "http")},
	})
}

// The pair is one finding, not two, so the route that sorts first stays clean.
// Gateway API's own last tiebreak is alphabetical, which makes the reported
// route the one that would lose it.
func (s *ConflictingRouteMatchTestSuite) TestReportsThePairOnceNotOnBothRoutes() {
	s.addConflictingPair()

	diagnostics := 0
	for _, obj := range s.ctx.Objects() {
		diagnostics += len(checkFunc(s.ctx, obj))
	}
	s.Equal(1, diagnostics)
}

// The case this check was validated against: the same hostname and path served
// on two listeners of one Gateway is a normal split, since a request arrives on
// one listener or the other and never both.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenTheRoutesSitOnDifferentListeners() {
	s.addGateway(httpListener("http", hostname), httpListener("https", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("https")}, hosts(hostname), prefix("/"))

	s.expect(nil)
}

// A parentRef with no sectionName attaches to every listener, so it overlaps a
// route that names one of them.
func (s *ConflictingRouteMatchTestSuite) TestFlagsWhenOneRouteNamesNoListener() {
	s.addGateway(httpListener("http", hostname), httpListener("https", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("")}, hosts(hostname), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("https")}, hosts(hostname), prefix("/"))

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/"`, "https")},
	})
}

// Two routes that both name no listener contend on every listener. The message
// names the first in sorted order so it does not move between runs.
func (s *ConflictingRouteMatchTestSuite) TestFlagsOnceWhenNeitherRouteNamesAListener() {
	s.addGateway(httpListener("https", hostname), httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("")}, hosts(hostname), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("")}, hosts(hostname), prefix("/"))

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/"`, "http")},
	})
}

func (s *ConflictingRouteMatchTestSuite) TestPassesWhenTheRoutesSitOnDifferentGateways() {
	s.addGateway(httpListener("http", hostname))
	other := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "other-gateway", Namespace: namespace},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "eg",
			Listeners:        []gatewayv1.Listener{httpListener("http", hostname)},
		},
	}
	other.SetGroupVersionKind(gvk("Gateway"))
	s.ctx.AddObject("gateway-other", other)

	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{{Name: "other-gateway"}}, hosts(hostname), prefix("/"))

	s.expect(nil)
}

// Gateway API ranks a longer path prefix ahead of a shorter one, so the author
// already said which route wins.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOnePathPrefixIsLonger() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/api"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/api/v2"))

	s.expect(nil)
}

func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOneMatchIsExactAndTheOtherAPrefix() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname),
		pathMatch(gatewayv1.PathMatchExact, "/checkout"))

	s.expect(nil)
}

// More header matches wins, so an extra header resolves the overlap.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOneRouteMatchesAnExtraHeader() {
	withHeader := prefix("/checkout")
	withHeader.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "x-canary", Value: "true"}}

	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), withHeader)

	s.expect(nil)
}

// HTTP header names are case insensitive, so a difference in spelling does not
// make two matches distinct.
func (s *ConflictingRouteMatchTestSuite) TestHeaderNameCasingDoesNotSeparateTheRoutes() {
	lower := prefix("/checkout")
	lower.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "x-canary", Value: "true"}}
	upper := prefix("/checkout")
	upper.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "X-Canary", Value: "true"}}

	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), lower)
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), upper)

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/checkout" and 1 header match`, "http")},
	})
}

// The order headers are written in carries no meaning.
func (s *ConflictingRouteMatchTestSuite) TestHeaderOrderDoesNotSeparateTheRoutes() {
	first := prefix("/checkout")
	first.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "x-a", Value: "1"}, {Name: "x-b", Value: "2"}}
	second := prefix("/checkout")
	second.Headers = []gatewayv1.HTTPHeaderMatch{{Name: "x-b", Value: "2"}, {Name: "x-a", Value: "1"}}

	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), first)
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), second)

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/checkout" and 2 header matches`, "http")},
	})
}

// A rule with no matches selects everything under "/", which is what the
// apiserver would have defaulted it to.
func (s *ConflictingRouteMatchTestSuite) TestRuleWithNoMatchesDefaultsToTheRootPrefix() {
	s.addGateway(httpListener("http", hostname))
	s.addRouteWithRules(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname),
		gatewayv1.HTTPRouteRule{})
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/"`, "http")},
	})
}

func (s *ConflictingRouteMatchTestSuite) TestPassesWhenTheHostnamesDiffer() {
	s.addGateway(httpListener("http", ""))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("a.example.com"), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("b.example.com"), prefix("/"))

	s.expect(nil)
}

// A longer hostname match wins, so a wildcard against an exact name is already
// decided.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOneHostnameIsAWildcard() {
	s.addGateway(httpListener("http", "*.example.com"))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("*.example.com"), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("shop.example.com"), prefix("/"))

	s.expect(nil)
}

// A route naming no hostname loses to one that names a hostname, so the two do
// not contend.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOnlyOneRouteNamesAHostname() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, nil, prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))

	s.expect(nil)
}

// Two routes that both name no hostname contend over whatever the listener
// serves.
func (s *ConflictingRouteMatchTestSuite) TestFlagsWhenNeitherRouteNamesAHostname() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, nil, prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, nil, prefix("/"))

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, "", `PathPrefix "/"`, "http")},
	})
}

// One shared hostname out of several is enough: requests to it match both.
func (s *ConflictingRouteMatchTestSuite) TestFlagsOnTheSharedHostnameOfSeveral() {
	s.addGateway(httpListener("http", ""))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")},
		hosts("a.example.com", "shared.example.com"), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")},
		hosts("shared.example.com", "b.example.com"), prefix("/"))

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, "shared.example.com", `PathPrefix "/"`, "http")},
	})
}

// A hostname the listener will not serve is not contested on it, whatever the
// routes claim. hostname-never-matches is the finding there, not this one.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenTheListenerDoesNotServeTheContestedHostname() {
	s.addGateway(httpListener("http", "a.example.com"))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("b.example.com"), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts("b.example.com"), prefix("/"))

	s.expect(nil)
}

// Gateway API breaks a remaining tie with the older creationTimestamp, which
// manifests read back from a cluster carry.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenCreationTimestampsDiffer() {
	s.addConflictingPair()
	for _, obj := range s.ctx.Objects() {
		switch obj.K8sObject.GetName() {
		case alphaRoute:
			obj.K8sObject.SetCreationTimestamp(metav1.NewTime(time.Unix(1000, 0)))
		case betaRoute:
			obj.K8sObject.SetCreationTimestamp(metav1.NewTime(time.Unix(2000, 0)))
		}
	}

	s.expect(nil)
}

// One timestamp is not a tiebreak, since the other route's age is unknown.
func (s *ConflictingRouteMatchTestSuite) TestFlagsWhenOnlyOneRouteCarriesACreationTimestamp() {
	s.addConflictingPair()
	for _, obj := range s.ctx.Objects() {
		if obj.K8sObject.GetName() == alphaRoute {
			obj.K8sObject.SetCreationTimestamp(metav1.NewTime(time.Unix(1000, 0)))
		}
	}

	s.expect(map[string][]string{
		betaRoute: {want(betaRoute, alphaRoute, hostname, `PathPrefix "/checkout"`, "http")},
	})
}

// The guard for partial manifest sets: routes commonly live in a different
// chart from the Gateway, and without the Gateway's listeners there is no way
// to tell whether the two routes share one.
func (s *ConflictingRouteMatchTestSuite) TestStaysQuietWhenTheGatewayIsAbsent() {
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/checkout"))

	s.expect(nil)
}

// A listener that admits no HTTPRoute is not a listener the routes contend on.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenTheListenerAcceptsNoHTTPRoutes() {
	listener := httpListener("http", hostname)
	listener.AllowedRoutes = &gatewayv1.AllowedRoutes{
		Kinds: []gatewayv1.RouteGroupKind{{Kind: "GRPCRoute"}},
	}
	s.addGateway(listener)
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))
	s.addRoute(betaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))

	s.expect(nil)
}

// A listener admitting only its own namespace leaves a route from elsewhere
// unattached, so it never contends.
func (s *ConflictingRouteMatchTestSuite) TestPassesWhenOneRouteIsNotAdmittedFromItsNamespace() {
	s.addGateway(httpListener("http", hostname))
	s.addRoute(alphaRoute, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))

	elsewhere := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: betaRoute, Namespace: "tenant"},
		Spec: gatewayv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{{
				Name:        gatewayName,
				Namespace:   func() *gatewayv1.Namespace { n := gatewayv1.Namespace(namespace); return &n }(),
				SectionName: sectionPtr("http"),
			}}},
			Hostnames: hosts(hostname),
			Rules:     []gatewayv1.HTTPRouteRule{{Matches: []gatewayv1.HTTPRouteMatch{prefix("/")}}},
		},
	}
	elsewhere.SetGroupVersionKind(gvk("HTTPRoute"))
	s.ctx.AddObject(betaRoute, elsewhere)

	s.expect(nil)
}

// A route kind without HTTP matches is out of scope: GRPCRoute precedence is a
// different set of rules.
func (s *ConflictingRouteMatchTestSuite) TestIgnoresNonHTTPRoutes() {
	s.addGateway(httpListener("http", hostname))
	for _, name := range []string{alphaRoute, betaRoute} {
		route := &gatewayv1.GRPCRoute{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: gatewayv1.GRPCRouteSpec{
				CommonRouteSpec: gatewayv1.CommonRouteSpec{ParentRefs: []gatewayv1.ParentReference{parentRef("http")}},
				Hostnames:       hosts(hostname),
				Rules:           []gatewayv1.GRPCRouteRule{{}},
			},
		}
		route.SetGroupVersionKind(gvk("GRPCRoute"))
		s.ctx.AddObject(name, route)
	}

	s.expect(nil)
}

// A route with three peers reports each pair once, from the route that sorts
// last of the two.
func (s *ConflictingRouteMatchTestSuite) TestReportsEveryConflictingPeer() {
	s.addGateway(httpListener("http", hostname))
	for _, name := range []string{"a-route", "b-route", "c-route"} {
		s.addRoute(name, []gatewayv1.ParentReference{parentRef("http")}, hosts(hostname), prefix("/"))
	}

	s.expect(map[string][]string{
		"b-route": {want("b-route", "a-route", hostname, `PathPrefix "/"`, "http")},
		"c-route": {
			want("c-route", "a-route", hostname, `PathPrefix "/"`, "http"),
			want("c-route", "b-route", hostname, `PathPrefix "/"`, "http"),
		},
	})
}
