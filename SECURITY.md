# Security policy

## Reporting a vulnerability

Please don't open a public issue for a security problem.

Use [GitHub's private vulnerability reporting](https://github.com/sdOps/gwlint/security/advisories/new) instead, from the repo's **Security** tab ("Report a vulnerability").
This opens a private advisory visible only to you and the maintainers, so the issue can be discussed and fixed before it's public.

We'll acknowledge a new report within a few days and follow up with next steps once we've assessed it.

## Scope

gwlint is a static analysis tool that reads Kubernetes manifests from local disk and prints findings; it does not run a server, accept network input, or execute anything it lints.
The most relevant vulnerability classes here are things like: a manifest that is not trusted input still causing memory exhaustion, a panic, or arbitrary code execution while being parsed; or a dependency with a known, reachable vulnerability (tracked via `mise run vulncheck` and `.govulncheck-allowlist`).

## Supported versions

gwlint is pre-1.0 and does not yet maintain multiple release branches.
Fixes land on `main` and go out in the next tagged release; there's no backport policy at this stage.
