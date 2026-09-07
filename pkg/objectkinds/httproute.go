package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// HTTPRoute represents Gateway API HTTPRoute objects.
const HTTPRoute = "HTTPRoute"

var httpRouteGVK = schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("HTTPRoute")

func init() {
	objectkinds.RegisterObjectKind(HTTPRoute, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == httpRouteGVK
	}))
}
