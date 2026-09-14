# Contributing

Thanks for looking at gwlint.
This file covers the mechanics of sending a change.
`AGENTS.md` is the real working guide: build and test commands, package layout, the architecture's constraints, and the Gateway API details that have caused real bugs.
Read it first; this file assumes it.

## Before you start

Open an issue for anything beyond a small fix, so the approach can be agreed before you write the code.
For a new check, the README's Checks section shows what's already covered and how checks are grouped into tiers, so a new one lands in the right spot.

## Making a change

1. Fork the repo and branch from `main`.
2. Make the change. Follow the conventions in `AGENTS.md`, in particular:
   - One package per check, under `pkg/templates/<checkname>/`, wired into `pkg/templates/all` and `pkg/builtinchecks/yamls/`.
   - A failing and a passing example under `examples/`, self-contained on its own and when the whole tree is linted together.
   - Reproduce a bug end to end through `gwlint lint` before fixing it, and prove a new test can fail by reverting the fix and watching it go red.
3. Run the local gate before opening a pull request:

   ```bash
   mise run check
   ```

   This runs the build, `go vet`, a Go-version-pin check, formatting, tests, and `golangci-lint`.
   It is exactly what CI runs, so a clean `mise run check` means CI should pass too.

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/): `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `ci:`, imperative subject, sentence case after the prefix.
Explain why in the body, including what was rejected and why.
`cliff.toml` and the release workflow both key off these prefixes to build the changelog and pick the next version, so the type has to be right, not just close.

`.github/workflows/pr-title.yml` enforces this on the pull request title, not on individual commits: this repo squash-merges, so the title is what actually becomes the commit message on `main`.

Commits on `main` must be signed; GitHub will tell you if a commit in your pull request isn't.
See [GitHub's guide to commit signing](https://docs.github.com/en/authentication/managing-commit-signature-verification) if you haven't set this up before.

## Pull requests

Open the pull request against `main`.
CI runs the same checks as `mise run check`, split across a test matrix (Linux, macOS, Windows), a lint job, and a vulnerability scan; all of them need to pass before merge.
Describe what changed and why; if the change fixes a bug, say how you reproduced it.

## Releases

Versions are not cut automatically. A maintainer triggers `.github/workflows/release.yml` by hand (`workflow_dispatch`) when `main` is ready to become a tagged version.
That workflow computes the version from Conventional Commit history using `git-cliff`, unless a version is given explicitly, and publishes a GitHub Release with generated notes.
Contributors don't need to do anything for this beyond writing properly typed commit messages.

## Reporting a vulnerability

See `SECURITY.md`. Don't open a public issue for a security problem.
