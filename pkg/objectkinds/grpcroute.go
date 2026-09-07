package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// GRPCRoute represents Gateway API GRPCRoute objects.
const GRPCRoute = "GRPCRoute"

var grpcRouteGVK = schema.GroupVersion{Group: gatewayv1.GroupName, Version: gatewayv1.GroupVersion.Version}.WithKind("GRPCRoute")

func init() {
	objectkinds.RegisterObjectKind(GRPCRoute, objectkinds.MatcherFunc(func(gvk schema.GroupVersionKind) bool {
		return gvk == grpcRouteGVK
	}))
}
