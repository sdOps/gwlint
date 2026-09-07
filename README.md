# gwlint

gwlint is a static analysis tool for Kubernetes Gateway API resources (`Gateway`, `HTTPRoute`, `GRPCRoute`, `ReferenceGrant`) and Envoy Gateway's own CRDs (`BackendTrafficPolicy`, `Backend`).
It catches semantic misconfigurations that schema validators can't see: a route conflict, a missing health check, a DNS resolution mode that doesn't fit a given backend.

Schema validators like kubeconform confirm a manifest is structurally valid.
Semantic linters like [kube-linter](https://github.com/stackrox/kube-linter) check things like "container runs as root," but have no Gateway API awareness (see [kube-linter#555](https://github.com/stackrox/kube-linter/issues/555), open since April 2023).
gwlint fills that gap.

gwlint is built by importing kube-linter's own Go packages as a library and adding Gateway API and Envoy Gateway checks on top, so the check logic can be upstreamed into kube-linter later with minimal rework: same `check.Template` shape, same package-per-check layout, same registration pattern.

## Scope

This first pass covers core Gateway API resources plus Envoy Gateway's CRDs specifically, not every Gateway API implementation's vendor extensions.
See `INSTRUCTION.md` for the full project brief, including a Phase 3 plan to validate the architecture against a second implementation (Istio is the leading candidate) once the Envoy-specific check set is further along.

## Checks

### `fqdn-backend-cold-start`

Flags a `BackendTrafficPolicy` with no active or passive health check configured, when that policy covers an `HTTPRoute` or `GRPCRoute` whose `backendRefs` point at an Envoy Gateway `Backend` object with an FQDN endpoint (`spec.endpoints[].fqdn`).
A policy covers a route either by targeting it directly, or by targeting the `Gateway` the route is attached to via `parentRefs`: Gateway API's policy attachment model applies a Gateway-level policy to every route on that Gateway by default, and this check follows that same resolution, since a shared Gateway-level policy with no health check is at least as common in practice as a route-level one.

**Why this check exists.** An FQDN-based backend endpoint puts Envoy Gateway on the same DNS-resolving cluster path the legacy Envoy `STRICT_DNS` cluster type used: the upstream address resolves at startup, not from a static IP.
This check is based on a real incident: a cold-start DNS resolution gap on an FQDN-backed route caused 503s before the name had resolved, with no health check in place to route around the gap.
The fix in that incident, and the remediation this check recommends, is an active or passive health check on the `BackendTrafficPolicy`, so a request has somewhere else to go while the name is still resolving.

**What it does not cover (known limitations for this pass):**

- `targetRef`/`targetRefs` naming an `HTTPRoute` or `GRPCRoute` are followed directly, and a `targetRef`/`targetRefs` naming a `Gateway` is resolved to every route whose `parentRefs` attach to that Gateway.
  `ListenerSet` targets (a newer, less common attachment point) and `TargetSelectors` (label-based targeting) are not resolved.
- Cross-namespace `backendRef`s are matched by namespace/name only; `ReferenceGrant` validity is not checked (that's a separate Phase 2 check).
- A health check being present does not mean the risk is actually closed: for a `Backend` with only one FQDN endpoint and no redundancy, ejecting or failing that endpoint has nowhere else to route to.
  The check only verifies that a health check exists, not that it provides real failover; a health check on a single-endpoint backend mainly changes the failure mode from slow to fast, it doesn't add availability.

**Remediation:** add a `healthCheck.active` or `healthCheck.passive` block to the `BackendTrafficPolicy`, or switch the `Backend` to an IP-based endpoint if the FQDN target isn't actually required.
See the [Envoy Gateway health check docs](https://gateway.envoyproxy.io/docs/api/extension_types/#healthcheck).

## Usage

```sh
gwlint lint ./path/to/manifests
gwlint lint --format json ./path/to/manifests
gwlint lint ./gateway-chart-render ./routes-chart-render
gwlint templates list
```

Output format matches kube-linter's own: plain text by default, `--format json` for machine-readable output.
Exit code is non-zero when lint errors are found, matching kube-linter's convention.

### Linting across multiple charts, directories, or repos

Gateway API resources are routinely authored across separate files: a platform team's Gateway and BackendTrafficPolicy chart, a different team's HTTPRoute chart, sometimes in entirely separate repos.
kube-linter's own `lintcontext` scopes each object to the directory it was loaded from, so a check that needs to correlate a policy with the route it covers would silently see nothing if the two live in different directories, even when passed to the same `gwlint lint` invocation.

gwlint's `lint` command deliberately merges every discovered context into one before running checks, so `gwlint lint <path1> <path2> ...` correlates objects across however many directories, Helm charts, or Helm releases they came from.
This matters because `helm template --output-dir` (and `helmfile template --output-dir`) both write one subdirectory per release, so even a single repo's rendered output is typically several directories deep; merging means you can point gwlint at the top of that output tree without flattening it by hand first.

A practical pattern for two separate repos, each managed by Helmfile with per-environment values:

```sh
helmfile -e prod template --skip-deps --output-dir /tmp/render/gwapi   # in the Gateway/policy repo
helmfile -e prod template --skip-deps --output-dir /tmp/render/routes  # in the routes repo, matching environment
gwlint lint /tmp/render/gwapi /tmp/render/routes
```

One caveat this implies: object identity (namespace + name + kind) must be genuinely unique across everything passed to one `gwlint lint` invocation, the same way it would need to be in a real cluster.
Linting two genuinely unrelated environments or clusters together in one invocation (rather than one repo's chart split across directories) isn't a supported use case, since a coincidental name collision between them would be treated as a real match.

See `examples/failing` and `examples/passing`, each grouped into subdirectories by scenario:

- `examples/failing/direct-route`: policy targets the `HTTPRoute` directly, no health check, FQDN backend.
- `examples/failing/gateway-level`: policy targets the `Gateway`, no health check, a route attached to that Gateway has an FQDN backend.
- `examples/passing/health-check-configured`: same as `direct-route`, but with a passive health check configured.
- `examples/passing/non-fqdn-backend`: no health check, but the backend is IP-based, not FQDN.
- `examples/passing/gateway-not-attached`: policy targets a `Gateway`, but the FQDN-backed route is attached to a different `Gateway`, so the policy never covers it.
- `examples/passing/service-backend`: no health check, but the route's `backendRef` is a plain core `Service` (the common case), not an Envoy Gateway `Backend`, so there's no FQDN endpoint to find.

## Building

gwlint pins its Go toolchain and lint tooling via [mise](https://mise.jdx.dev/) (see `mise.toml`), which also defines the common dev commands as mise tasks:

```sh
mise install            # install the pinned Go toolchain and golangci-lint
mise run build           # build ./gwlint
mise run test            # go test ./...
mise run vet             # go vet ./...
mise run lint            # golangci-lint run ./...
mise run check           # build, vet, test, and lint together
mise run lint-examples   # build, then run gwlint against examples/failing and examples/passing
```

Equivalent plain commands, if you'd rather not use the tasks:

```sh
mise exec -- go build -o gwlint ./cmd/gwlint
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- golangci-lint run ./...
```

### A note on dependency versions

gwlint's `go.mod` carries `replace` directives pinning `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, and `helm.sh/helm/v3` down to the versions kube-linter v0.8.3 itself depends on.
This is necessary, not incidental: kube-linter v0.8.3 hard-imports `k8s.io/api/autoscaling/{v2beta1,v2beta2}`, which were removed from `k8s.io/api` in v0.36.0, while Envoy Gateway v1.9.1 requires `k8s.io/api` >= v0.36.3 for its own (unused by gwlint) Helm chart tooling.
Without the `replace` directives, Go's minimum-version-selection would pick the newer, incompatible version and kube-linter's own source would fail to compile.
Revisit these pins whenever either dependency is upgraded.

## Architecture

gwlint imports kube-linter's engine packages (`pkg/check`, `pkg/config`, `pkg/lintcontext`, `pkg/objectkinds`, `pkg/templates`, `pkg/diagnostic`, `pkg/checkregistry`, `pkg/run`) rather than forking kube-linter's source.

A few things kube-linter doesn't expose a way to extend from outside its own module, so gwlint reimplements them:

- **Decoding.** kube-linter's own YAML decoder (built in an unexported `init()`) can't be extended from outside its package, and `lintcontext.CreateContextsWithOptions`'s `CustomDecoder` option is a full replacement, not an overlay.
  `pkg/decoder` builds a decoder starting from the same base `k8s.io/client-go` scheme kube-linter itself starts from, layering Gateway API's and Envoy Gateway's own types on top.
- **Default check enablement.** Registering a `check.Template` via `templates.Register` is not enough on its own for kube-linter's engine to run a check by default; the actual enabled-by-default scope comes from a `config.Check` entry.
  `pkg/builtinchecks` mirrors kube-linter's own `pkg/builtinchecks` package: an embedded YAML config per check, loaded into a `checkregistry.CheckRegistry`.
- **The `lint` CLI command.** kube-linter's own `lint` command hardcodes `lintcontext.CreateContexts` with no way to supply a custom decoder, so `pkg/command/lint` reimplements that command's flow.
  It reuses kube-linter's own output-formatting and flag-parsing helpers (`pkg/command/common`, and `ValidateAndPairFormatsOutputs`/`NewOutputDestination` from `pkg/command/lint`) everywhere they don't hardcode kube-linter's own decoder.
  `pkg/command/root` reuses kube-linter's own `templates` subcommand as-is, since it operates on the shared template registry and correctly lists only what gwlint has registered into it.
- **Context merging.** kube-linter scopes each `LintContext` to the directory its files were loaded from, one per directory.
  gwlint's checks need to correlate objects across files that are routinely split into separate directories, charts, or repos, so `pkg/command/lint`'s `mergeContexts` (see `merge.go`) combines every discovered context into one before checks run.
  See "Linting across multiple charts, directories, or repos" above for why this matters and what it assumes.

## Licensing and attribution

gwlint is built using [kube-linter](https://github.com/stackrox/kube-linter)'s engine, licensed under the Apache License, Version 2.0.
gwlint is not an official kube-linter product and is not affiliated with or endorsed by the kube-linter project.
See `LICENSE` for the full Apache 2.0 text and `NOTICE` for attribution details.
