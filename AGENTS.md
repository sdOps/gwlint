# AGENTS.md

## Overview

gwlint is a static analysis tool for Kubernetes Gateway API and Envoy Gateway manifests, written in Go.
It finds semantic misconfigurations that schema validators cannot see: a manifest can be structurally valid and still be wrong.

It is built by importing kube-linter's engine packages as a library rather than forking them, so check logic can be upstreamed into kube-linter later with minimal rework.
That constraint shapes most of the architecture below.

The README's Checks section tracks coverage: what's implemented, grouped into tiers by how badly the failure it catches hides.
Read it before starting a new check, and add a row when one lands.

## Build and test

The repo pins its toolchain with [mise](https://mise.jdx.dev/) (`mise.toml`). Use the tasks rather than raw commands so you get the pinned versions:

```bash
mise run check            # build, vet, check-go-version, fmt-check, test, lint. The gate.
mise run build            # build ./gwlint
mise run test             # go test ./...
mise run test-race        # go test -race ./...
mise run lint             # golangci-lint run ./...
mise run fmt-check        # golangci-lint fmt --diff ./...
mise run lint-examples    # build, then run the binary against examples/
mise run vulncheck        # govulncheck, gated by .govulncheck-allowlist
mise run check-go-version # fail if mise.toml and go.mod disagree

go test ./pkg/templates/fqdnbackendcoldstart/ -run TestFQDNBackendColdStart   # one package
```

`mise run check` is what CI runs. A change is not done until it passes.

## Code structure

```
cmd/gwlint/                       entry point; blank-imports each check package so its init() registers
pkg/command/root/                 CLI assembly; reuses kube-linter's templates subcommand as-is
pkg/command/lint/                 the lint command, plus merge.go for cross-directory context merging
pkg/decoder/                      runtime.Decoder covering core k8s + every served Gateway API version + Envoy Gateway
pkg/gatewayapi/                   shared route model: flattens every route kind and version into one shape
pkg/objectkinds/                  kube-linter object-kind registrations (gatewayapi.go, backend.go, backendtrafficpolicy.go)
pkg/builtinchecks/                embedded default check configs; yamls/<check-name>.yaml per check
pkg/templates/all/                blank-imports every check; import this, not individual check packages
pkg/templates/<checkname>/        one package per check
pkg/version/                      gwlint's version string
examples/failing/<scenario>/      manifests a check must flag
examples/passing/<scenario>/      manifests a check must not flag
```

Inside a check package, split by job once it grows:
`template.go` for registration and the check function, and separate files for resolution logic (see `fqdnbackendcoldstart`, which has `routes.go` and `coverage.go`).

## Constraints

These are not preferences.

- **Never design around one deployment's shape.** This is the single most important rule here and the one most often broken.
  Enumerate what the API actually permits, not what the manifests in front of you happen to use.
  For target kinds, read the upstream CRD's kubebuilder validation rules, which are authoritative:
  Envoy Gateway's `api/v1alpha1/*_types.go` `XValidation` lines list every valid `targetRef` kind.
  Cover every served API version, not just the newest.
  Where something genuinely cannot be resolved, document it as a limitation in the README rather than leaving it silently unhandled.
  A linter that exits clean on a broken manifest is worse than no linter.
- **Never state something in the README you have not verified.** Run the command, read the type, fetch the URL.
  Claims here have been wrong three separate times: "static binary", "helm `--output-dir` writes one directory per release", and "these CVEs have no published fix".
  Each survived review because it sounded right.
- **Reproduce a bug end to end before fixing it**, as manifests through `gwlint lint`, the way a user hits it. Do not start from a unit test; you will fix the wrong thing.
- **Prove a new test can fail.** Revert the fix, confirm the test goes red, restore it. An assertion that never failed is not evidence.
- **Never hand-edit `.govulncheck-allowlist` without the reasoning.** Each entry states why the advisory is unreachable.
  An entry whose reason is wrong is worse than no entry, because it looks like somebody checked.
- **This repo does not belong to any single employer.** Do not apply an employer's internal conventions here: no private GitHub Enterprise host, no organization-specific PR ruleset or bypass label, no assumption that a skill or workflow tied to one company's tooling applies.
- **Do not add `Co-authored-by:` trailers** to commits.

## Conventions

Match kube-linter's own Go conventions for anything that may be upstreamed:

- One package per check, under `pkg/templates/<checkname>/`.
- The same `check.Template` and `check.Func` shapes, registered via `templates.Register` in `init()`.
- A `check.Template` registration alone does not make the engine run a check.
  It also needs a `config.Check` entry in `pkg/builtinchecks/yamls/`, and the check package must be added to `pkg/templates/all`.
- Route extraction belongs in `pkg/gatewayapi`, not in a check package. More than one check walks parentRefs and backendRefs.
- A check that resolves references needs a guard for the partial manifest set: linting routes without the chart that defines the Gateway is normal, so judge a reference only once the set contains something of that kind to judge against.
- Comments explain why, not what, at the density kube-linter's own templates use.
- No speculative abstraction. Generalize on the second or third real case, not the first.

Commit messages: [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `ci:`), imperative subject, sentence case after the prefix.
Explain why in the body, including what was rejected and why.
Earlier commits in this repo predate the convention and are not in this format; match it for new commits regardless.
`cliff.toml` and the release workflow both key off these prefixes to build the changelog, so getting the type right matters beyond style.

Prose in Markdown: one sentence per line.

## What the architecture works around

kube-linter does not expose extension points for several things, so gwlint reimplements them. Do not "simplify" these back.

- **Decoding.** kube-linter's decoder is built in an unexported `init()` and cannot be extended; `lintcontext.Options.CustomDecoder` is a full replacement, not an overlay.
  `pkg/decoder` starts from the same client-go base scheme and layers Gateway API (v1, v1beta1, v1alpha2, v1alpha3) and Envoy Gateway on top.
- **Default check enablement.** `pkg/builtinchecks` mirrors kube-linter's own package: an embedded YAML `config.Check` per check, loaded into a `checkregistry.CheckRegistry`.
- **The lint command.** kube-linter's own hardcodes `lintcontext.CreateContexts` with no way to supply a decoder.
  `pkg/command/lint` reimplements that flow while reusing its output formatting and flag helpers.
- **Context merging.** kube-linter scopes a `LintContext` to the directory its files came from.
  Gateway API objects are routinely split across directories, charts, and repos, so `mergeContexts` combines every discovered context into one before checks run.
  Without it, any cross-object check silently sees nothing.

## Gateway API domain notes

Hard-won, and each one was a real bug:

- A `backendRef` that omits a namespace defaults to **the route's** namespace, not the policy's. They differ whenever a Gateway-level policy reaches a route in another namespace.
- `sectionName` on a policy `targetRef` means **two different things**: a listener on a `Gateway` or `ListenerSet`, and a rule on a route. Applying one as the other silently matches nothing.
- A `parentRef` with no `sectionName` attaches the route to **every** listener, so a listener-scoped policy still covers it.
- With no `mergeType` set, Envoy Gateway applies **only the most specific** policy, so a route-level policy displaces an inherited Gateway-level one.
- The attachment hierarchy is Gateway to ListenerSet to Route. A Gateway-level policy reaches routes attached to a ListenerSet belonging to that Gateway.
- `TCPRoute`, `TLSRoute` and `UDPRoute` carry `backendRefs` exactly like HTTP and gRPC routes do, and are usually authored as `v1alpha2`. `ReferenceGrant` is usually `v1beta1`.

## Testing

- Every check needs a failing case and a passing case at minimum, plus a scenario directory under `examples/`.
- Each `examples/` scenario must be self-contained and demonstrate exactly one finding, both on its own and when the whole tree is linted at once. Adding a check can light up other scenarios that were previously incomplete; check every directory in isolation afterwards.
- Unit tests use `lintcontext/mocks`. **`MockLintContext.Objects()` iterates a map**, so never index into it or assert on diagnostic ordering.
  `Validate` compares with `ElementsMatch`, which is order-independent.
- End-to-end tests in `pkg/command/lint/command_test.go` run the real command over `examples/`. Adding an example means updating the expected finding count there.
- Keep object names unique across all of `examples/`, since contexts are merged and identity is namespace plus name plus kind.

## CI

Three workflows, all using SHA-pinned actions:

- `ci.yml`: test matrix across ubuntu, macos and windows, plus Linux-only examples, lint and tidy jobs.
- `vulncheck.yml`: govulncheck on push, PR, and nightly. Separate because the vulnerability database changes without the repo changing.
- `next-go.yml`: weekly canary against the newest stable Go. Gates nothing. Run it before bumping the Go pin.

Note that `golangci-lint` v2 splits linting and formatting: `run` does **not** report formatting problems, only `fmt` does. Both have to pass, which is why `fmt-check` exists.

## Dependency pins

`go.mod` carries `replace` directives pinning `k8s.io/api`, `k8s.io/apimachinery`, `k8s.io/client-go`, and `helm.sh/helm/v3` down to the versions kube-linter v0.8.3 itself depends on.
This is necessary, not incidental: kube-linter v0.8.3 hard-imports `k8s.io/api/autoscaling/{v2beta1,v2beta2}`, which were removed from `k8s.io/api` in v0.36.0, while Envoy Gateway v1.9.1 requires `k8s.io/api` >= v0.36.3 for its own (unused by gwlint) Helm chart tooling.
Without the `replace` directives, Go's minimum-version-selection would pick the newer, incompatible version and kube-linter's own source would fail to compile.
Revisit these pins whenever kube-linter or Envoy Gateway is upgraded.

## Environment quirks

Some development machines have Go configured for a private GitHub Enterprise host (`GOPROXY=direct`, `GOPRIVATE` set to that host), which cannot resolve public modules.
`go mod tidy`, `go get` and `govulncheck` need an override in this repo:

```bash
GOPROXY='https://proxy.golang.org,direct' GOPRIVATE='' go mod tidy
```

CI is unaffected.

## Status

27 checks, covering core Gateway API (Tiers 1 through 4) and Envoy Gateway (Tier 5).
The README's Status section is the source of truth for what is done and what is left; `dangling-extension-ref` is the one remaining check.

The repo is private but is intended to be made public, so write user-facing docs for an outside reader.
Before that happens it still needs a release process (`pkg/version/version.go` is a hardcoded constant, not stamped at link time), tagged versions, a `SECURITY.md`, and a `CONTRIBUTING.md`.
