# gwlint

gwlint is a static analysis tool for Kubernetes Gateway API and Envoy Gateway manifests.
It parses every Gateway API route kind (`HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute`) plus `Gateway`, `ListenerSet` and `ReferenceGrant`, at every version each is served at, alongside Envoy Gateway's own `BackendTrafficPolicy` and `Backend`, and correlates them with each other rather than judging any one object alone.
It's for semantic misconfigurations that schema validators can't see: things a manifest can be perfectly valid and still get wrong, like a backend that resolves via DNS at startup with no health check to cover the gap.

Schema validators like kubeconform confirm a manifest is structurally valid.
Semantic linters like [kube-linter](https://github.com/stackrox/kube-linter) check things like "container runs as root," but have no Gateway API awareness (see [kube-linter#555](https://github.com/stackrox/kube-linter/issues/555), open since April 2023).
gwlint fills that gap.

gwlint is built by importing kube-linter's own Go packages as a library and adding Gateway API and Envoy Gateway checks on top, so the check logic can be upstreamed into kube-linter later with minimal rework: same `check.Template` shape, same package-per-check layout, same registration pattern.

## Status

This is Phase 1: one check, `fqdn-backend-cold-start`, implemented end to end and documented below.
It's a vertical slice meant to prove the architecture before building out the rest of the rule set, not the full intended scope.
Checks are scoped to `BackendTrafficPolicy` and to all five route kinds; `Gateway`, `ListenerSet` and `Backend` are read while resolving them, and nothing reads `ReferenceGrant` yet.
Route conflict detection, missing-`ReferenceGrant` detection, retry policy sanity, and missing passive health checks are planned next (Phase 2); see `INSTRUCTION.md` for the full backlog and a Phase 3 plan to validate the architecture against a second Gateway API implementation once the Envoy-specific check set is further along.

## Scope

Checks fall into two groups.
Vendor-neutral checks use only core Gateway API types and work against any implementation; `dangling-parent-ref` is one.
Vendor-specific checks key off an implementation's own CRDs, and today that means Envoy Gateway's; `fqdn-backend-cold-start` is one.
This first pass covers core Gateway API resources plus Envoy Gateway's CRDs specifically, not every implementation's vendor extensions.

## Checks

### `dangling-parent-ref`

Flags a route whose `parentRefs` name a `Gateway` or `ListenerSet` that is not present.

A route attaches to a Gateway by naming it, and nothing rejects a name that does not resolve.
The manifests stay schema-valid, the apply succeeds, and the route is simply never programmed, so its traffic is never served.
A typo or a rename on the Gateway side produces exactly this, silently.

This check is vendor-neutral: it uses only core Gateway API types and works against any implementation.
It covers all five route kinds, and resolves a `parentRef` the way Gateway API does, defaulting an omitted kind to `Gateway` and an omitted namespace to the route's own.

**What it does not cover:** linting a routes-only manifest set is normal, since the Gateway usually lives in another chart or repo.
If the set contains no `Gateway` or `ListenerSet` at all, every `parentRef` would look dangling, so the check stays quiet rather than flagging everything.
Pass the Gateway's manifests in the same invocation to get real coverage; see "Linting across multiple charts, directories, or repos" below.
A `parentRef` into another API group is left alone, since it belongs to some other implementation's attachment model.

**Remediation:** correct the `parentRef`, or include the manifests that define the Gateway in the same `gwlint` invocation.

### `fqdn-backend-cold-start`

Flags a `BackendTrafficPolicy` with no active or passive health check configured, when that policy covers a route whose `backendRefs` point at an Envoy Gateway `Backend` object with one or more FQDN endpoints (`spec.endpoints[].fqdn.hostname`).
Every FQDN endpoint on the backend is named in the finding, since each one is a name that has to resolve before traffic can flow.
A policy covers a route in any of the ways Envoy Gateway lets it: by naming the route in `targetRef`/`targetRefs`, by selecting it with `targetSelectors`, or by targeting the `Gateway` or `ListenerSet` the route attaches to via `parentRefs`.
Gateway API's policy attachment hierarchy runs Gateway to ListenerSet to Route, so a Gateway-level policy applies to every route beneath it by default, including routes attached to a `ListenerSet` belonging to that Gateway; this check follows that same resolution, since a shared Gateway-level policy with no health check is at least as common in practice as a route-level one.
All five route kinds are resolved, not just `HTTPRoute` and `GRPCRoute`: `TCPRoute`, `TLSRoute` and `UDPRoute` carry `backendRefs` too, so they reach FQDN `Backend`s the same way.

A `sectionName` on the `targetRef` narrows coverage, and means different things by target kind, both of which are honoured: on a `Gateway` or `ListenerSet` it names a listener, so only routes attached to that listener are covered (a route whose `parentRefs` name no listener attaches to all of them, so it stays covered); on a route it names a rule, so only that rule's `backendRefs` are.

Policy precedence is applied too. Envoy Gateway's `mergeType` documentation is explicit that with no merge configured, "only the most specific configuration takes effect", so a `Gateway`-level policy with no health check is not reported for a route that has its own `BackendTrafficPolicy` configuring one: the more specific policy displaces it and the gap is already closed. A route-level policy that is itself missing a health check overrides nothing, so the Gateway-level finding still stands.

**Why this check exists.** An FQDN-based backend endpoint puts Envoy Gateway on the same DNS-resolving cluster path the legacy Envoy `STRICT_DNS` cluster type used: the upstream address resolves at startup, not from a static IP.
This check is based on a real incident: a cold-start DNS resolution gap on an FQDN-backed route caused 503s before the name had resolved, with no health check in place to route around the gap.
The fix in that incident, and the remediation this check recommends, is an active or passive health check on the `BackendTrafficPolicy`, so a request has somewhere else to go while the name is still resolving.

**What it does not cover (known limitations for this pass):**

- A `targetSelectors` entry scoped to namespaces by label (`namespaces.from: Selector`) is not resolved, since deciding it needs the `Namespace` objects, which a rendered manifest set does not usually contain. `from: Same` (the default) and `from: All` are both honoured.
- A `backendRef` that omits a namespace resolves in the namespace of the route that references it, which for a Gateway-level policy is not necessarily the policy's own namespace.
  Cross-namespace `backendRef`s are still matched by namespace/name only; `ReferenceGrant` validity is not checked (that's a separate Phase 2 check), so a reference that a real cluster would reject for want of a grant is still reported here.
- Policy precedence is resolved structurally, by asking whether a more specific policy configures a health check at all. Envoy Gateway's `mergeType` (which merges a route-level policy into its parent rather than replacing it) is not interpreted, so a policy using it may be judged as a plain override.
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

Every run ends with a line naming what it actually checked:

```
No lint errors found.
Checked 2 BackendTrafficPolicy objects (98 objects loaded from 38 files).
```

The first number is the one worth reading.
kube-linter decodes a kind it does not recognise into an unstructured object rather than rejecting it, so objects load whether or not gwlint understands them, and a large loaded count on its own does not mean a check ever ran.
When nothing matched, the line says what was missing instead of reporting a clean result that was never really checked:

```
No BackendTrafficPolicy objects found, so nothing was checked (2 objects loaded from 2 files).
```

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

- `examples/failing/dangling-parent-ref`: a route whose `parentRefs` names a Gateway that is not there.
- `examples/passing/parent-ref-resolves`: the same shape, with the Gateway present.
- `examples/failing/direct-route`: policy targets the `HTTPRoute` directly, no health check, FQDN backend.
- `examples/failing/gateway-level`: policy targets the `Gateway`, no health check, a route attached to that Gateway has an FQDN backend.
- `examples/failing/cross-namespace-gateway`: platform team's Gateway-level policy in one namespace, an application team's FQDN-backed route in another, attached across namespaces via `parentRefs`.
- `examples/failing/stream-route`: policy targets a `TCPRoute` authored as `v1alpha2`, with an FQDN backend and no health check.
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

## Contributing

`AGENTS.md` holds the working guide for this repo: the build and test commands, the package layout, what the architecture deliberately works around, the Gateway API details that have caused real bugs, and the rules a change has to meet.
It is written for coding agents, but it is the same information a new human contributor needs, so start there.

## Continuous integration

`.github/workflows/ci.yml` runs on every push to `main` and on every pull request:

- **Test** builds, vets, and runs the tests under the race detector, as a matrix across `ubuntu-latest`, `macos-latest` and `windows-latest`.
  Path handling is the thing worth testing across platforms, and the tests are full of it: the end-to-end tests write manifests into temporary directories and lint them through the real command, so they exercise the same file walking a user's invocation does.
  The matrix does not fail fast, so one platform breaking still reports the others.
- **Examples** runs the compiled binary against `examples/failing` and `examples/passing` and checks each exits the way it should. Linux only, because the task is a shell script; the end-to-end test in the matrix already drives the same command over the same manifests everywhere.
- **Lint** runs `golangci-lint run` and `golangci-lint fmt --diff`, then checks that `mise.toml` and `go.mod` still name the same Go version.
- **go.mod is tidy** runs `go mod tidy` and fails if `go.mod` or `go.sum` changed.
`.github/workflows/vulncheck.yml` runs `govulncheck` separately, on pushes and pull requests but also nightly and on demand.
It is its own workflow because it is the only check whose result can change without the repository changing: govulncheck fetches the vulnerability database at every run, so an advisory published against an unchanged dependency would otherwise go unnoticed until somebody happened to push.
It fails on anything reachable from gwlint's own code that is not recorded in `.govulncheck-allowlist`, and equally fails on an entry there that is no longer reported.
That file lists what is currently suppressed and why; it is the one place that has to stay accurate, so it is not repeated here.

`.github/workflows/next-go.yml` builds and tests against the newest stable Go release, weekly and on demand.
gwlint pins one Go version, so nothing else would notice a newer toolchain breaking the build until somebody tried to bump the pin; this makes that upgrade a known quantity instead of a discovery.
It is deliberately not part of CI and does not gate anything, because a regression in an upstream Go release is not a reason to block a pull request.
Run it from the Actions tab before changing the version in `go.mod`.

Unlike helm's govulncheck workflow, the vulnerability workflow is not filtered to `go.sum` changes: govulncheck answers whether gwlint's own code reaches a vulnerable symbol, so a source change can alter the result with no dependency change at all.

Every action is pinned to a commit SHA rather than a tag, with the version in a trailing comment, so a moved tag cannot change what CI runs.

The Linux-only jobs install their toolchain with mise from the same `mise.toml` a developer uses, so a green local `mise run check` means the same tool versions ran locally as in CI.
The test matrix uses `actions/setup-go` instead, reading the version from `go.mod`: mise does not document Windows support, and `mise run check-go-version` keeps `go.mod` and `mise.toml` on the same number, so the pin stays single-sourced either way.

The Go version lives in `go.mod`, and `mise.toml` pins the toolchain to the same number; `mise run check-go-version` fails if they drift apart.
`.golangci.yml` deliberately sets no `go:` version of its own, since golangci-lint reads it from `go.mod`.

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
