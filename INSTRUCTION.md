# gwlint: Gateway API Semantic Linter

## How to use this document

This is a project brief, not a task list to execute blindly.
Phase 0 is investigation and must be completed and reported back before any check logic is written.
Do not skip ahead to Phase 1 on assumptions about how kube-linter's internals work.
Verify each claim below against the actual source before relying on it.

## One-line summary

Build `gwlint`, a static analysis tool for Kubernetes Gateway API resources (HTTPRoute, Gateway, GRPCRoute, BackendTrafficPolicy, ReferenceGrant) that catches semantic misconfigurations, not just schema errors.
It is built by importing kube-linter's own Go packages as a library and adding Gateway API-specific checks on top, so it can later be upstreamed into kube-linter with minimal rework.

## Background

Two categories of tool already exist for Kubernetes manifests, and neither covers this ground.

Schema validators like kubeconform confirm a manifest is structurally valid against the OpenAPI or CRD schema.
They cannot tell you a route conflicts with another route, that a BackendTrafficPolicy has no health check, or that a DNS resolution mode is a bad fit for a given backend.

Semantic linters like kube-linter check things like "container runs as root" or "no CPU limits set," but only for core Kubernetes and a handful of common CRDs.
Gateway API support was requested in kube-linter as issue #555 in April 2023 and has never been implemented; the current built-in check list (as of this writing) has zero Gateway API awareness.

That gap is what `gwlint` fills.
The initial rule set should draw from real incidents: a STRICT_DNS cold-start failure causing 503s on SSO routes, and retry/health-check gaps found during Gateway API adoption work.

## Non-goals for this phase

Do not build an admission webhook or a controller.
Do not attempt to support every Gateway API implementation's vendor extensions (Istio, Cilium, Kong) in the first pass; scope to Envoy Gateway's CRDs plus core Gateway API resources.
Do not reimplement schema validation; kube-linter already wraps kubeconform for that via its `schema-validation` check, and `gwlint` should not duplicate it.
Do not build a plugin/dynamic-loading system; Go doesn't support this well, and kube-linter itself doesn't do it. Checks are compiled in.

## Architecture decisions (already made)

`gwlint` imports kube-linter's core packages as a Go module dependency rather than forking kube-linter's source.
kube-linter is Apache 2.0 licensed and published as `golang.stackrox.io/kube-linter`, so this is permitted; see the Licensing section below for the compliance obligations that come with it.

Packages to depend on:

- `golang.stackrox.io/kube-linter/pkg/check` (the `Template` and `Func` types)
- `golang.stackrox.io/kube-linter/pkg/config` (config file schema, custom check definitions)
- `golang.stackrox.io/kube-linter/pkg/lintcontext` (parsed manifest objects, cross-object lookups)
- `golang.stackrox.io/kube-linter/pkg/objectkinds` (GVK registration and matching)
- `golang.stackrox.io/kube-linter/pkg/templates` (the check registry)
- `golang.stackrox.io/kube-linter/pkg/diagnostic` (result/output types)

New checks are written as Go packages under `pkg/templates/<checkname>/` inside the `gwlint` repo, each registering itself via `templates.Register(check.Template{...})` in an `init()` function, matching kube-linter's own package shape exactly (see `pkg/templates/pdbmaxunavailable` in kube-linter's source for a real reference example, already reviewed during design).
This is a deliberate choice, not a style preference: if the shape is identical to kube-linter's own templates, upstreaming later is a matter of moving the package and fixing the import path, not rewriting logic.

Gateway API GVKs (Gateway, HTTPRoute, GRPCRoute, ReferenceGrant) are registered as custom object kinds using kube-linter's existing `objectkinds.RegisterObjectKind` mechanism, which is explicitly documented as the extension point for CRDs not built into kube-linter.
BackendTrafficPolicy is an Envoy Gateway-specific CRD (`gateway.envoyproxy.io` group), not core Gateway API; its Go types live in Envoy Gateway's own module, not `sigs.k8s.io/gateway-api`. Confirm the exact module path in Phase 0 rather than assuming.

## Phase 0: investigation (do this first, report findings before writing checks)

1. Clone `github.com/stackrox/kube-linter` locally and read `cmd/kube-linter/main.go` plus whatever package constructs its Cobra command tree.
   Determine whether that command construction is exported and importable as a library, or whether it's a plain `main` package not meant to be imported.
   This determines whether `gwlint`'s own CLI is a thin wrapper around kube-linter's existing command tree, or a small Cobra setup of its own on top of the same engine packages. Either is fine; we need to know which before writing the CLI.

2. Read `pkg/lintcontext` end to end.
   Understand how manifests are parsed off disk and decoded into typed Go objects, and what's required to add a new GVK to that pipeline (a decoder registration, a scheme addition, or something else).

3. Read three existing kube-linter templates as reference implementations before writing anything new: `pdbmaxunavailable` (simplest full example), `dangling-service` or `mismatching-selector` (cross-object lookups, the pattern route-conflict detection will need), and `non-existent-service-account` (reference-validity checking, the pattern ReferenceGrant validation will need).

4. Confirm exact module paths and current versions for: `golang.stackrox.io/kube-linter`, `sigs.k8s.io/gateway-api` (for Gateway, HTTPRoute, GRPCRoute, ReferenceGrant types), and the Envoy Gateway module that defines BackendTrafficPolicy.

5. Confirm kube-linter's current license file and NOTICE file contents verbatim, so attribution can be set up correctly in Phase 1.

Report back what you found for each of these before proceeding. If anything above turns out to be wrong (for example, if templates cannot be registered from an external module due to unexported internals), stop and flag it rather than working around it silently, since it may change the whole architecture.

### Phase 0 findings (confirmed 2026-09-07)

`root.Command()` in `golang.stackrox.io/kube-linter/pkg/command/root` is exported and returns a `*cobra.Command`.
gwlint can wrap it directly rather than building a separate Cobra tree.

GVK support is two independent mechanisms, not one, and the brief's original line calling out only `objectkinds.RegisterObjectKind` named the wrong one for decoding.
Turning YAML into typed Gateway API structs requires a custom `runtime.Decoder`, built from client-go's base scheme plus `sigs.k8s.io/gateway-api`'s `AddToScheme` and Envoy Gateway's `AddToScheme`, passed into kube-linter via `lintcontext.Options.CustomDecoder`.
This is a full replacement of kube-linter's own decoder, so gwlint will not inherit kube-linter's OpenShift, KEDA, or Prometheus-operator scheme support; that is expected and out of scope.
`objectkinds.RegisterObjectKind` is the separate, second step: it only controls which already-decoded objects get routed to a given check's `Func`.

Registering a `check.Template` is not enough on its own for a check to run by default.
kube-linter uses an embedded YAML layer (`pkg/builtinchecks`) to set each check's actual default scope and enablement, overriding the template's `SupportedObjectKinds` field.
gwlint needs an equivalent mechanism as a Phase 1 deliverable; this was missing from the original "required for done" list below and has been added.

Confirmed module versions: `golang.stackrox.io/kube-linter` v0.8.3, `sigs.k8s.io/gateway-api` v1.6.2 (`Gateway`, `HTTPRoute`, `GRPCRoute`, `ReferenceGrant` are all GA in `apis/v1`), Envoy Gateway `github.com/envoyproxy/gateway` v1.9.1 (types in `api/v1alpha1`, group `gateway.envoyproxy.io/v1alpha1` as assumed).

kube-linter ships a stock, unfilled Apache 2.0 `LICENSE` file and no `NOTICE` file.
gwlint's attribution section should be its own README or NOTICE content crediting kube-linter, not a copy of a NOTICE that does not exist upstream.

Important finding that changes Phase 1's check logic: `BackendTrafficPolicy` has no literal `STRICT_DNS`/`LOGICAL_DNS` field in current Envoy Gateway.
Envoy Gateway picks the DNS-cluster path automatically based on backend type; an FQDN-based `Backend`/`BackendEndpoint.Hostname` triggers what the code itself calls "equivalent to the legacy STRICT_DNS type."
The literal `STRICT_DNS`/`LOGICAL_DNS` enum only exists at the raw Envoy xDS level (`Cluster.DiscoveryType`), not on the CRD.
Phase 1's recommended check below has been revised accordingly.

## Phase 1: vertical slice

Implement exactly one check, fully, end to end, before building out the rest of the rule set. This proves the architecture works before investing in breadth.

Recommended first check: **FQDN-backend cold-start risk** (revised from the original "DNS resolution mode" framing after Phase 0 found no literal `STRICT_DNS`/`LOGICAL_DNS` field on `BackendTrafficPolicy`; see Phase 0 findings above).
Flag a `BackendTrafficPolicy` with no active or passive health check configured, whose `targetRef`/`targetRefs` names an `HTTPRoute` or `GRPCRoute` that has a `backendRef` to an Envoy Gateway `Backend` object with an FQDN endpoint (`spec.endpoints[].fqdn.hostname`), since an FQDN endpoint is what triggers Envoy Gateway's automatic STRICT_DNS-equivalent cluster path.
Recommend adding a health check or an explicit startup dependency instead.

This is a cross-object check, not the single-object check originally assumed: `BackendTrafficPolicy` never references a backend directly (confirmed against `policy_helpers.go`; its `targetRef` only names a `Gateway`/`HTTPRoute`/`GRPCRoute`/etc., and `Backend` is not a valid target kind), so detecting "this policy covers an FQDN backend" requires the three-hop traversal above: policy targetRef, to the route it names, to that route's backendRef, to the `Backend` object's FQDN endpoint.
This is the same cross-object lookup pattern as `dangling-service`/`non-existent-service-account` from Phase 0, just one hop deeper.
It still maps to the real incident (cold-start 503s before DNS resolves), just scoped to fields Envoy Gateway's CRD actually exposes rather than one that does not exist, and confirmed as needing cross-referencing rather than being single-object.

Required for this check to count as done:

- Template registered via `templates.Register`, following kube-linter's `check.Template` shape: `HumanName`, `Key`, `Description`, `SupportedObjectKinds`, remediation text with a link to the relevant Envoy Gateway docs page.
- An embedded default-check-config layer equivalent to kube-linter's `pkg/builtinchecks` (a `//go:embed`-loaded YAML `config.Check` entry setting this check's actual scope and enabled-by-default status), since a `check.Template` registration alone does not make kube-linter's engine run the check. See Phase 0 findings above.
- A custom `runtime.Decoder` wired through `lintcontext.Options.CustomDecoder`, layering `sigs.k8s.io/gateway-api`'s and Envoy Gateway's `AddToScheme` onto client-go's base scheme, so `BackendTrafficPolicy` and core Gateway API objects actually decode off disk. See Phase 0 findings above.
- Unit tests covering both the failing case and at least one passing case, using a lint context fixture in the style of kube-linter's `lintcontext/mocks` package.
- `gwlint lint <path>` runs against a sample manifest directory and produces output in the same shape kube-linter uses (plain text by default, `--format json` supported), so the tool feels familiar to anyone who already uses kube-linter.
- README documents the check, why it exists, and links to the incident pattern it's based on.

## Phase 2: backlog (do not start until Phase 1 is fully done and reviewed)

- Retry policy sanity: missing retry budget, no `maxRetries` cap, retries configured on non-idempotent methods without an idempotency guard.
- Route conflict detection: two HTTPRoutes attached to the same Gateway with overlapping path matches and no explicit precedence.
- Missing ReferenceGrant: HTTPRoute referencing a backend Service in a different namespace with no matching ReferenceGrant.
- Missing health check: BackendTrafficPolicy with no passive health check configured.
- Version-skew warnings: manifest uses a field the installed Envoy Gateway version doesn't support yet (requires a way to know the target Envoy Gateway version, design this before starting).

## Phase 3: multi-vendor validation (do not start until Phase 2 backlog items are addressed)

gwlint's checks fall into two groups.
Vendor-neutral checks, such as route conflict detection and missing ReferenceGrant, operate only on core Gateway API types and already work against any implementation.
Vendor-specific checks, such as the FQDN-backend cold-start check, retry policy sanity, and missing health check, key off Envoy Gateway's own CRDs and are Envoy-only by construction.

Before treating gwlint's architecture as validated for eventual upstreaming into kube-linter, implement one equivalent vendor-specific check for a second Gateway API implementation.
This is the real test of whether the package-per-check, compiled-in pattern generalizes across vendors, not just across checks within one vendor.

Istio is the leading candidate for the second implementation, given it has the widest Gateway API deployment footprint.
Its retry and health-check configuration lives in different CRDs than Envoy Gateway's (DestinationRule and related types, not BackendTrafficPolicy), so this phase requires its own Phase-0-style investigation before any check logic is written: confirm the exact CRDs, module paths, and field names against Istio's actual source, the same way Phase 0 did for Envoy Gateway.
Do not assume Istio's concepts map one-to-one onto Envoy Gateway's.

Do not pick a second vendor other than Istio without re-evaluating deployment footprint at the time this phase starts.
Phase 3 is out of scope for the current initial engagement; it is recorded here so the goal isn't lost.

## Testing requirements

Every check needs a failing-case test and a passing-case test at minimum.
Run `go test ./...`, `go vet`, and whatever linter kube-linter itself uses (check their `.golangci.yml`, match it rather than inventing a different config) before considering any check done.
Do not claim a check works without having actually run it against a real sample manifest, not just the unit test fixtures.

## Licensing and attribution

Include a full copy of kube-linter's Apache 2.0 license text in the `gwlint` repo.
Preserve copyright notices in any code copied or closely adapted from kube-linter (not just imported as a dependency).
Add a NOTICE file or a README attribution section crediting kube-linter and linking to its repo.
Do not use "KubeLinter" branding or name `gwlint` in a way that implies it is an official kube-linter product; "built using kube-linter's engine" is accurate and fine.

## Coding conventions

Match kube-linter's own Go conventions for anything that will eventually be upstreamed: package-per-check layout, the same `check.Template` and `check.Func` shapes, the same parameter-description pattern for configurable checks.
Comments explain why, not what, matching the density already present in kube-linter's own templates.
No speculative abstraction ahead of the second or third check; get the vertical slice right first, then generalize only what actually repeats.

## Definition of done for this initial engagement

Phase 0 findings reported and any architecture risks flagged.
Phase 1 vertical slice merged: one working check, tested, documented, running through a real CLI invocation with real output.
Licensing and attribution in place.
Nothing from Phase 2 started yet.
Phase 3 not started; it is recorded above only so the multi-vendor validation goal is not lost.
