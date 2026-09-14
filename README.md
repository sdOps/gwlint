# gwlint

gwlint is a static analysis tool for Kubernetes Gateway API and Envoy Gateway manifests.
It parses every Gateway API route kind (`HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute`) plus `Gateway`, `ListenerSet` and `ReferenceGrant`, at every version each is served at, alongside Envoy Gateway's own `BackendTrafficPolicy` and `Backend`, and correlates them with each other rather than judging any one object alone.
It's for semantic misconfigurations that schema validators can't see: things a manifest can be perfectly valid and still get wrong, like a backend that resolves via DNS at startup with no health check to cover the gap.

Schema validators like kubeconform confirm a manifest is structurally valid.
Semantic linters like [kube-linter](https://github.com/stackrox/kube-linter) check things like "container runs as root," but have no Gateway API awareness (see [kube-linter#555](https://github.com/stackrox/kube-linter/issues/555), open since April 2023).
gwlint fills that gap by importing kube-linter's own Go packages as a library and adding Gateway API and Envoy Gateway checks on top, so the check logic can be upstreamed into kube-linter later with minimal rework.

## Status

gwlint runs 27 of 28 identified checks, grouped below into five tiers ordered by how badly the failure they catch hides: a misconfiguration that makes a manifest invalid gets caught by a schema validator, and one that applies cleanly and silently serves nothing is what gwlint is for.
One check, `dangling-extension-ref`, remains undone: a filter's `extensionRef` can name an object of any kind, including ones gwlint has no typed knowledge of, and it needs its own design pass.

## Scope

Vendor-neutral checks (Tiers 1-4, 23 checks) use only core Gateway API types and work against any implementation.
Vendor-specific checks (Tier 5, 4 checks) key off an implementation's own CRDs; Envoy Gateway is the only one covered so far.
Adding another implementation (Istio, Cilium, Kong, GKE Gateway, ...) means a new Tier 5 package keying off that implementation's own CRDs, not a rewrite: the vendor-neutral tiers already work against it today.

Schema validity and workload-level checks (like "container runs as root") are out of scope on purpose: kubeconform and kube-linter already own those.

## Checks

`gwlint templates list` prints the current set with the same key and description gwlint ships with.
Every check has a matching failing/passing pair under `examples/`, and its own package (`pkg/templates/<checkname>/template.go`) documents why it exists, why a schema validator misses it, and what it deliberately doesn't cover.

**Tier 1: silent non-attachment** - the route exists, the apply succeeds, the status conditions say why, and nobody reads status conditions.

| Check | Scope | Flags |
|---|---|---|
| `listener-not-found` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose parentRefs sectionName names a listener the Gateway does not declare, so the route is never accepted onto it. |
| `route-not-permitted` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes a listener will not admit because its allowedRoutes excludes the route's namespace or kind, so the route never attaches. |
| `hostname-never-matches` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose hostnames cannot intersect the hostname of any listener they attach to, so no request can ever match the route. |
| `protocol-mismatch` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes attached to a listener whose protocol cannot carry their kind, such as an HTTPRoute on a TCP listener, so the route is never programmed. |
| `dangling-parent-ref` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose parentRefs name a Gateway or ListenerSet that is not present, so the route is never programmed. |

**Tier 2: reference integrity** - a reference that resolves to nothing. Applies cleanly, fails at request time.

| Check | Scope | Flags |
|---|---|---|
| `dangling-backend-ref` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose backendRefs name a Service or Envoy Gateway Backend that is not present, so the route resolves and then serves errors for that backend. |
| `missing-reference-grant` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose backendRefs cross a namespace boundary with no ReferenceGrant in the target namespace permitting the reference, so the backend never resolves. |
| `missing-certificate-grant` | `Gateway`, `ListenerSet` | Flag TLS listeners whose certificateRefs cross into another namespace with no ReferenceGrant permitting it, so the listener is never programmed. |
| `dangling-certificate-ref` | `Gateway`, `ListenerSet` | Flag TLS listeners whose certificateRefs name a Secret that is not present, so the listener is never programmed and TLS handshakes on it fail. |
| `dangling-gateway-class` | `Gateway` | Flag Gateways whose gatewayClassName names a GatewayClass that is not present, so no controller claims the Gateway and none of its listeners are ever programmed. |
| `dangling-listener-set-parent` | `ListenerSet` | Flag ListenerSets whose parentRef names a Gateway that is not present, so their listeners are never merged into a Gateway and never bound. |
| `dangling-policy-target` | `BackendTrafficPolicy` | Flag BackendTrafficPolicies whose targetRefs name a Gateway, ListenerSet or route that is not present, so the policy attaches to nothing and its settings never apply. |

**Tier 3: route semantics** - the route attaches and resolves, but does not do what it looks like it does.

| Check | Scope | Flags |
|---|---|---|
| `conflicting-route-match` | `HTTPRoute` | Flag two HTTPRoutes that claim the same hostname with an identical request match on the same listener, where Gateway API's precedence rules leave the winner undefined. |
| `all-weights-zero` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag route rules that set weight 0 on every backendRef, so the rule matches requests and then forwards them to nothing. |
| `rule-serves-nothing` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag route rules with no backendRefs and no filter that produces a response, so requests matching the rule are answered with an error. |
| `duplicate-rule-name` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes with two rules sharing a name, which a policy targetRef sectionName cannot then address unambiguously. |
| `service-backend-without-port` | `HTTPRoute`, `GRPCRoute`, `TCPRoute`, `TLSRoute`, `UDPRoute` | Flag routes whose backendRefs name a core Service with no port, which Gateway API requires, so the reference does not resolve. |

**Tier 4: Gateway and listener validity**

| Check | Scope | Flags |
|---|---|---|
| `duplicate-listener-name` | `Gateway`, `ListenerSet` | Flag Gateways and ListenerSets that declare two listeners under the same name, which a route sectionName or a listener-scoped policy cannot then address unambiguously. |
| `conflicting-listeners` | `Gateway`, `ListenerSet` | Flag listeners sharing a port that Gateway API cannot tell apart, either because their protocols cannot share a port or because port, protocol and hostname are identical. |
| `tls-listener-without-certificate` | `Gateway` | Flag Gateway listeners that terminate TLS with no tls.certificateRefs, so the listener has no certificate to present and every handshake fails. |
| `hostname-on-non-hostname-protocol` | `Gateway` | Flag Gateway listeners that set a hostname on TCP or UDP, where Gateway API ignores it and the listener keeps admitting every connection on its port. |
| `gateway-serves-no-routes` | `Gateway` | Flag Gateways that no route in the manifest set attaches to, so the Gateway is provisioned and serves no traffic. |

**Tier 5: Envoy Gateway** - only after the core is covered.

| Check | Scope | Flags |
|---|---|---|
| `fqdn-backend-cold-start` | `BackendTrafficPolicy` | Flag BackendTrafficPolicies with no active or passive health check covering a route backed by an FQDN Backend endpoint, which resolves via DNS at startup and can 503 before the name resolves. |
| `health-check-without-failover` | `BackendTrafficPolicy` | Flag BackendTrafficPolicies whose health check covers a route served by a single Backend endpoint, where ejecting it leaves nowhere to fail over to. |
| `retry-without-budget` | `BackendTrafficPolicy` | Flag BackendTrafficPolicies that set spec.retry.numRetries with no per-retry timeout and no backoff interval, so retries can amplify load on a backend that is already failing. |
| `backend-without-endpoints` | `Backend` | Flag Envoy Gateway Backends that declare no endpoints, so every backendRef resolving to them has nothing to route to. |
| `conflicting-policies` | `BackendTrafficPolicy` | Flag two BackendTrafficPolicies targeting the same object and section, where Envoy Gateway attaches only one and silently rejects the rest as Conflicted. |

## Installation

gwlint is a single self-contained binary with no third-party runtime dependencies.

### Prebuilt binary

Each [release](https://github.com/sdOps/gwlint/releases) publishes cross-compiled binaries for linux, darwin and windows (amd64 and arm64, except windows/arm64) plus a `SHA256SUMS` file covering all of them:

```sh
curl -LO https://github.com/sdOps/gwlint/releases/download/<version>/gwlint-<version>-linux-amd64
curl -LO https://github.com/sdOps/gwlint/releases/download/<version>/SHA256SUMS
sha256sum -c SHA256SUMS --ignore-missing
chmod +x gwlint-<version>-linux-amd64
```

### go install

Needs Go 1.26.8 or newer.

```sh
go install github.com/sdOps/gwlint/cmd/gwlint@latest
```

This puts `gwlint` in `$(go env GOBIN)`, or in `$(go env GOPATH)/bin` when `GOBIN` is unset; make sure whichever one applies is on your `PATH`.

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

A prebuilt binary or a `mise run build` from a `git clone` reports the real version: `git describe` for a local build, or the tag itself for a release binary.
`go install` and a plain `go build`/`go run` report `dev` instead: `go install` builds from the module cache, which has no `.git` for `git describe` to read, and neither passes the `-ldflags` a version needs.

## Usage

```sh
gwlint lint ./path/to/manifests
gwlint lint --format json ./path/to/manifests
gwlint lint ./gateway-chart-render ./routes-chart-render
gwlint templates list
gwlint version
```

Output follows kube-linter's own shape: plain text by default, `--format json` for machine-readable output, non-zero exit code when a finding is reported.
Every run ends with a summary line naming what it actually checked, e.g. `Checked 2 BackendTrafficPolicy objects (98 objects loaded from 38 files)`, or `No BackendTrafficPolicy objects found, so nothing was checked` when none matched.
The loaded-object count on its own doesn't mean a check ran: kube-linter decodes a kind it doesn't recognize into an unstructured object rather than rejecting it, so objects load whether or not gwlint understands them.

### Linting across multiple charts, directories, or repos

Gateway API resources are routinely authored across separate files: a platform team's Gateway/policy chart, a different team's route chart, sometimes in entirely separate repos.
kube-linter's own `lintcontext` scopes each object to the directory it was loaded from, so a check that needs to correlate a policy with the route it covers would silently see nothing if the two live in different directories, even when passed to the same invocation.

gwlint's `lint` command merges every discovered context into one before running checks, so `gwlint lint <path1> <path2> ...` correlates objects across however many directories, Helm charts, or Helm releases they came from.
This matters because rendering to a directory never produces one flat pile of YAML: `helm template --output-dir` writes a subdirectory per chart, and `helmfile template --output-dir` writes one per release.

```sh
helm template gateway-release ./chart -f values-prod.yaml --output-dir /tmp/render/gateway   # in the Gateway/policy repo
helm template routes-release  ./chart -f values-prod.yaml --output-dir /tmp/render/routes    # in the routes repo, same environment's values

gwlint lint /tmp/render/gateway /tmp/render/routes
```

Helmfile works the same way: render each repo's environment (`helmfile -e production template --skip-deps --output-dir ...`) to its own directory, then pass every directory to one `gwlint lint` call. A loop over repos keeps this from growing unwieldy as more route-owning repos show up.

Object identity (namespace + name + kind) must be genuinely unique across everything passed to one invocation, the same way it would need to be in a real cluster; this isn't meant for linting two unrelated environments together in one call.

See `examples/failing` and `examples/passing` for a scenario per check, e.g. `examples/failing/dangling-parent-ref` and `examples/passing/parent-ref-resolves`.
Each can be linted on its own or together (`gwlint lint ./examples`); every object name is kept unique across the whole tree so aggregate runs don't cross-contaminate.
`ls examples/failing examples/passing` for the full list.

## Building

gwlint pins its Go toolchain and lint tooling via [mise](https://mise.jdx.dev/) (see `mise.toml`):

```sh
mise install             # install the pinned Go toolchain and golangci-lint
mise run check           # build, vet, version check, fmt-check, test, and lint - the local gate CI also runs
mise run build           # build ./gwlint
mise run test            # go test ./...
mise run test-race       # go test -race ./...
mise run lint            # golangci-lint run ./...
mise run fmt             # apply gofmt and goimports formatting
mise run fmt-check       # fail if anything is unformatted
mise run vulncheck       # govulncheck, gated by .govulncheck-allowlist
mise run lint-examples   # build, then run gwlint against examples/failing and examples/passing
```

golangci-lint v2 splits linting and formatting: `run` does not report formatting problems, only `fmt` does, which is why `fmt-check` exists as its own task.

## Contributing

See `CONTRIBUTING.md` for the process, and `AGENTS.md` for the working guide: build and test commands, package layout, and the Gateway API details that have caused real bugs.

## Continuous integration

- `ci.yml`: test matrix across `ubuntu-latest`, `macos-latest` and `windows-latest`, plus Linux-only examples, lint, and `go mod tidy` jobs, on every push to `main` and every pull request.
- `vulncheck.yml`: `govulncheck` on push, PR, and nightly - separate because the vulnerability database can change without the repo changing.
- `next-go.yml`: weekly canary against the newest stable Go. Gates nothing; run it before bumping the Go pin.

All actions are pinned to a commit SHA rather than a tag. See `AGENTS.md` for more on what each workflow does and why, including the `go.mod` version pins kube-linter's own dependencies require.

## Architecture

gwlint imports kube-linter's engine packages as a library rather than forking kube-linter's source, and reimplements the few things kube-linter doesn't expose an extension point for: decoding, default check enablement, the `lint` command itself, and cross-directory context merging (see "Linting across multiple charts, directories, or repos" above).
See `AGENTS.md`'s "What the architecture works around" for the detail and reasoning behind each.

## Licensing and attribution

gwlint is built using [kube-linter](https://github.com/stackrox/kube-linter)'s engine, licensed under the Apache License, Version 2.0.
gwlint is not an official kube-linter product and is not affiliated with or endorsed by the kube-linter project.
See `LICENSE` for the full Apache 2.0 text and `NOTICE` for attribution details.
