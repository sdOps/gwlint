# gwlint

gwlint is a static analysis tool for Kubernetes Gateway API and Envoy Gateway manifests.
It parses `Gateway`, `HTTPRoute`, `GRPCRoute` and `ReferenceGrant` alongside Envoy Gateway's own `BackendTrafficPolicy` and `Backend`, and correlates them with each other rather than judging any one object alone.
It's for semantic misconfigurations that schema validators can't see: things a manifest can be perfectly valid and still get wrong, like a backend that resolves via DNS at startup with no health check to cover the gap.

Schema validators like kubeconform confirm a manifest is structurally valid.
Semantic linters like [kube-linter](https://github.com/stackrox/kube-linter) check things like "container runs as root," but have no Gateway API awareness (see [kube-linter#555](https://github.com/stackrox/kube-linter/issues/555), open since April 2023).
gwlint fills that gap.

gwlint is built by importing kube-linter's own Go packages as a library and adding Gateway API and Envoy Gateway checks on top, so the check logic can be upstreamed into kube-linter later with minimal rework: same `check.Template` shape, same package-per-check layout, same registration pattern.

## Status

This is Phase 1: one check, `fqdn-backend-cold-start`, implemented end to end and documented below.
It's a vertical slice meant to prove the architecture before building out the rest of the rule set, not the full intended scope.
Of the kinds gwlint parses, only `BackendTrafficPolicy` has a check scoped to it today; `Gateway`, `HTTPRoute`, `GRPCRoute` and `Backend` are read while resolving that check, and nothing reads `ReferenceGrant` yet.
Route conflict detection, missing-`ReferenceGrant` detection, retry policy sanity, and missing passive health checks are planned next (Phase 2); see `INSTRUCTION.md` for the full backlog and a Phase 3 plan to validate the architecture against a second Gateway API implementation once the Envoy-specific check set is further along.

## Scope

This first pass covers core Gateway API resources plus Envoy Gateway's CRDs specifically, not every Gateway API implementation's vendor extensions.

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

## Installation

gwlint is a single self-contained binary with no third-party runtime dependencies. Building it needs Go 1.26.8 or newer.

### go install

```sh
go install github.com/sdOps/gwlint/cmd/gwlint@latest
```

This puts `gwlint` in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin` when `GOBIN` is unset; make sure whichever one applies is on your `PATH`.

The repository is private today, so `go install` only works if you have access to it and Go is configured to fetch it directly rather than through the public module proxy:

```sh
export GOPRIVATE=github.com/sdOps/*
```

That step goes away once the repository is public.

### From source

```sh
git clone https://github.com/sdOps/gwlint.git
cd gwlint
go build -o gwlint ./cmd/gwlint
```

Or, if you use [mise](https://mise.jdx.dev/), `mise install && mise run build` picks up the pinned Go toolchain from `mise.toml` instead of whatever Go you happen to have.

### Verifying

```sh
gwlint version        # prints the version
gwlint templates list # prints the checks this binary knows about
```

There are no tagged releases and no prebuilt binaries yet, so there is nothing to download and nothing to pin to.
`gwlint version` reports `0.1.0-dev` from every build regardless of which commit it came from, since the version is still a constant in `pkg/version/version.go` rather than stamped in at link time.
Both of those are release-process work that Phase 1 deliberately left alone.

## Usage

```sh
gwlint lint ./path/to/manifests
gwlint lint --format json ./path/to/manifests
gwlint lint ./gateway-chart-render ./routes-chart-render
gwlint templates list
gwlint version
```

Output follows kube-linter's own shape: plain text by default, `--format json` for machine-readable output.
Exit code is non-zero when lint errors are found, matching kube-linter's convention.

### Linting across multiple charts, directories, or repos

Gateway API resources are routinely authored across separate files: a platform team's Gateway and BackendTrafficPolicy chart, a different team's HTTPRoute chart, sometimes in entirely separate repos.
kube-linter's own `lintcontext` scopes each object to the directory it was loaded from, so a check that needs to correlate a policy with the route it covers would silently see nothing if the two live in different directories, even when passed to the same `gwlint lint` invocation.

gwlint's `lint` command deliberately merges every discovered context into one before running checks, so `gwlint lint <path1> <path2> ...` correlates objects across however many directories, Helm charts, or Helm releases they came from.
This matters because rendering to a directory never produces one flat pile of YAML: `helm template --output-dir` writes a subdirectory per chart (`<output-dir>/<chart-name>/templates/...`, plus one more for every subchart), and `helmfile template --output-dir` writes one per release.
Even a single repo's rendered output is several directories deep, so merging means you can point gwlint at the top of that tree without flattening it by hand first.

**Plain Helm, one chart per repo.** Render each repo's chart to its own output directory with the same values file (or `--set` overrides) you'd actually deploy with, then point gwlint at both directories in one call:

```sh
helm template gateway-release  ./chart -f values-prod.yaml --output-dir /tmp/render/gateway   # in the Gateway/policy repo
helm template routes-release   ./chart -f values-prod.yaml --output-dir /tmp/render/routes    # in the routes repo, same environment's values

gwlint lint /tmp/render/gateway /tmp/render/routes
```

**Helmfile, environment-driven values across repos.** If each repo picks its manifests via a Helmfile environment rather than a single values file, render the same environment from each repo before linting:

```sh
helmfile -e production template --skip-deps --output-dir /tmp/render/gateway   # in the Gateway/policy repo
helmfile -e production template --skip-deps --output-dir /tmp/render/routes   # in the routes repo, matching environment

gwlint lint /tmp/render/gateway /tmp/render/routes
```

**More than two repos.** The pattern is the same regardless of count; a loop keeps it from growing unwieldy as more route-owning repos show up:

```sh
render_dir=/tmp/render
rm -rf "$render_dir" && mkdir -p "$render_dir"

for repo in gateway-repo routes-repo-a routes-repo-b; do
  (cd "$repo" && helmfile -e production template --skip-deps --output-dir "$render_dir/$repo")
done

gwlint lint "$render_dir"/*
```

That last line expands to one `gwlint lint` call listing every repo's rendered output directory, correlated as if it were all one cluster's manifests.

One caveat this implies: object identity (namespace + name + kind) must be genuinely unique across everything passed to one `gwlint lint` invocation, the same way it would need to be in a real cluster.
Linting two genuinely unrelated environments or clusters together in one invocation (rather than one real deployment's chart split across repos) isn't a supported use case, since a coincidental name collision between them would be treated as a real match.

See `examples/failing` and `examples/passing`, each grouped into subdirectories by scenario:

- `examples/failing/direct-route`: policy targets the `HTTPRoute` directly, no health check, FQDN backend.
- `examples/failing/gateway-level`: policy targets the `Gateway`, no health check, a route attached to that Gateway has an FQDN backend.
- `examples/passing/health-check-configured`: same as `direct-route`, but with a passive health check configured.
- `examples/passing/non-fqdn-backend`: no health check, but the backend is IP-based, not FQDN.
- `examples/passing/gateway-not-attached`: policy targets a `Gateway`, but the FQDN-backed route is attached to a different `Gateway`, so the policy never covers it.
- `examples/passing/service-backend`: no health check, but the route's `backendRef` is a plain core `Service` (the common case), not an Envoy Gateway `Backend`, so there's no FQDN endpoint to find.

Each scenario can be linted on its own, or together (`gwlint lint ./examples/failing`, `./examples/passing`, or `./examples` for all of them at once): every route, backend, and policy name is kept unique across the whole tree specifically so aggregate runs don't cross-contaminate now that contexts are merged (see above).

## Building

gwlint pins its Go toolchain and lint tooling via [mise](https://mise.jdx.dev/) (see `mise.toml`), which also defines the common dev commands as mise tasks:

```sh
mise install             # install the pinned Go toolchain and golangci-lint
mise run build           # build ./gwlint
mise run test            # go test ./...
mise run test-race       # go test -race ./...
mise run vet             # go vet ./...
mise run lint            # golangci-lint run ./...
mise run fmt             # apply gofmt and goimports formatting
mise run fmt-check       # fail if anything is unformatted
mise run vulncheck       # govulncheck, gated by .govulncheck-allowlist
mise run check           # build, vet, version check, fmt-check, test, and lint together
mise run lint-examples   # build, then run gwlint against examples/failing and examples/passing
```

`mise run check` is the local gate; CI runs the same tasks, with the pinned tool versions from the same `mise.toml`.
Note that golangci-lint v2 splits linting and formatting: `golangci-lint run` does not report formatting problems, only `golangci-lint fmt` does, which is why `fmt-check` exists as its own task.

Equivalent plain commands, if you'd rather not use the tasks:

```sh
mise exec -- go build -o gwlint ./cmd/gwlint
mise exec -- go test ./...
mise exec -- go vet ./...
mise exec -- golangci-lint run ./...
mise exec -- golangci-lint fmt --diff ./...
```

## Continuous integration

`.github/workflows/ci.yml` runs on every push to `main` and on every pull request, in four parallel jobs:

- **Build and test** builds the binary, runs `go vet`, runs the tests under the race detector, and then runs the compiled binary against `examples/failing` and `examples/passing`, checking each exits the way it should.
  The unit tests exercise the check through a lint context; this last step is the only thing that proves the shipped binary works on real manifests.
- **Lint** runs `golangci-lint run` and `golangci-lint fmt --diff`, then checks that `mise.toml` and `go.mod` still name the same Go version.
- **go.mod is tidy** runs `go mod tidy` and fails if `go.mod` or `go.sum` changed.
- **Vulnerabilities** runs `govulncheck` through `scripts/vulncheck.sh`.

CI installs its toolchain with mise from the same `mise.toml` a developer uses, so a green local `mise run check` means the same tool versions ran locally as in CI.
CI runs on Linux only; nothing currently exercises the macOS or Windows paths.

The Go version lives in `go.mod`, and `mise.toml` pins the toolchain to the same number; `mise run check-go-version` fails if they drift apart.
`.golangci.yml` deliberately sets no `go:` version of its own, since golangci-lint reads it from `go.mod`.

### Known vulnerabilities

`scripts/vulncheck.sh` fails on any vulnerability govulncheck can reach from gwlint's code unless it is listed in `.govulncheck-allowlist`, and equally fails on an allowlist entry that is no longer reported, so the file cannot quietly go stale.

Four entries are allowlisted today: one in `golang.org/x/crypto/openpgp`, which is deprecated outright rather than patched, and three in `github.com/containerd/containerd` (CVE-2026-53492, CVE-2026-50195, CVE-2026-53489).
The containerd three are fixed in 2.1.9 / 2.2.5 / 2.3.2 with no backport to the 1.7 line, and those fixes sit on the `github.com/containerd/containerd/v2` module path, which minimum version selection cannot reach from the v1 path gwlint is on.
helm v3.20.0 requires `containerd` v1.7.x, and `go.mod` pins helm to v3.20.0 to match kube-linter (see below), so helm is the actual blocker; the three come off the allowlist once helm moves to containerd v2.

All four reach gwlint only through the package `init()` of code linked in transitively by kube-linter's `lintcontext`, via helm's OCI registry support and containerd beneath it.
gwlint verifies no signatures and talks to no container runtime, so nothing gwlint's own code does drives those paths.
Each entry carries its reasoning in the allowlist file; re-check them whenever kube-linter or helm is upgraded.

### A note on dependency versions

gwlint's `go.mod` carries `replace` directives pinning `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, and `helm.sh/helm/v3` down to the versions kube-linter v0.8.3 itself depends on.
This is necessary, not incidental: kube-linter v0.8.3 hard-imports `k8s.io/api/autoscaling/{v2beta1,v2beta2}`, which were removed from `k8s.io/api` in v0.36.0, while Envoy Gateway v1.9.1 requires `k8s.io/api` >= v0.36.3 for its own (unused by gwlint) Helm chart tooling.
Without the `replace` directives, Go's minimum-version-selection would pick the newer, incompatible version and kube-linter's own source would fail to compile.
Revisit these pins whenever either dependency is upgraded.

## Architecture

gwlint imports kube-linter's engine packages (`pkg/check`, `pkg/config`, `pkg/lintcontext`, `pkg/objectkinds`, `pkg/templates`, `pkg/diagnostic`, `pkg/checkregistry`, `pkg/run`, `pkg/k8sutil`, `pkg/pathutil`) rather than forking kube-linter's source.

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
