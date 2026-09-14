// Package all imports every check template so that importing this one package
// registers the lot. Mirrors kube-linter's own pkg/templates/all.
//
// A check is only registered by the init() of its own package, so anything
// that runs checks has to import each one. Keeping that list here means adding
// a check touches one file rather than the binary and every test that needs a
// populated registry.
package all

import (
	// Each check package registers its template via init().
	_ "github.com/sdOps/gwlint/pkg/templates/allweightszero"
	_ "github.com/sdOps/gwlint/pkg/templates/backendwithoutendpoints"
	_ "github.com/sdOps/gwlint/pkg/templates/conflictinglisteners"
	_ "github.com/sdOps/gwlint/pkg/templates/conflictingpolicies"
	_ "github.com/sdOps/gwlint/pkg/templates/conflictingroutematch"
	_ "github.com/sdOps/gwlint/pkg/templates/danglingbackendref"
	_ "github.com/sdOps/gwlint/pkg/templates/danglingcertificateref"
	_ "github.com/sdOps/gwlint/pkg/templates/danglinggatewayclass"
	_ "github.com/sdOps/gwlint/pkg/templates/danglinglistenersetparent"
	_ "github.com/sdOps/gwlint/pkg/templates/danglingparentref"
	_ "github.com/sdOps/gwlint/pkg/templates/danglingpolicytarget"
	_ "github.com/sdOps/gwlint/pkg/templates/duplicatelistenername"
	_ "github.com/sdOps/gwlint/pkg/templates/duplicaterulename"
	_ "github.com/sdOps/gwlint/pkg/templates/fqdnbackendcoldstart"
	_ "github.com/sdOps/gwlint/pkg/templates/gatewayservesnoroutes"
	_ "github.com/sdOps/gwlint/pkg/templates/healthcheckwithoutfailover"
	_ "github.com/sdOps/gwlint/pkg/templates/hostnamenevermatches"
	_ "github.com/sdOps/gwlint/pkg/templates/hostnameonnonhostnameprotocol"
	_ "github.com/sdOps/gwlint/pkg/templates/listenernotfound"
	_ "github.com/sdOps/gwlint/pkg/templates/missingcertificategrant"
	_ "github.com/sdOps/gwlint/pkg/templates/missingreferencegrant"
	_ "github.com/sdOps/gwlint/pkg/templates/protocolmismatch"
	_ "github.com/sdOps/gwlint/pkg/templates/retrywithoutbudget"
	_ "github.com/sdOps/gwlint/pkg/templates/routenotpermitted"
	_ "github.com/sdOps/gwlint/pkg/templates/ruleservesnothing"
	_ "github.com/sdOps/gwlint/pkg/templates/servicebackendwithoutport"
	_ "github.com/sdOps/gwlint/pkg/templates/tlslistenerwithoutcertificate"
)
