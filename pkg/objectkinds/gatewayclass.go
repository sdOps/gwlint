package objectkinds

import (
	"golang.stackrox.io/kube-linter/pkg/objectkinds"
)

// GatewayClass represents Gateway API GatewayClass objects. It is cluster
// scoped, so a name on its own identifies one. Like the other Gateway API
// kinds it is matched across every served version, since v1beta1 is still
// served and which version a manifest uses is the author's choice.
const GatewayClass = "GatewayClass"

func init() {
	objectkinds.RegisterObjectKind(GatewayClass, gatewayAPIMatcher(GatewayClass))
}
