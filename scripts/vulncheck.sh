#!/bin/sh
# Run govulncheck and fail on any vulnerability that gwlint's own code can
# reach and that is not recorded in .govulncheck-allowlist with a reason.
#
# Also fails when an allowlist entry is no longer reported, so entries get
# removed once a fix lands upstream rather than accumulating forever.
set -eu

GOVULNCHECK_VERSION="v1.8.0"
ALLOWLIST=".govulncheck-allowlist"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# govulncheck exits non-zero when it finds anything, which is not an error here.
go run "golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}" ./... > "$work/report.txt" 2>&1 || true

if ! grep -q '^=== Symbol Results ===' "$work/report.txt"; then
  echo "govulncheck did not produce a report:" >&2
  cat "$work/report.txt" >&2
  exit 1
fi

# The symbol-results section lists only vulnerabilities gwlint's code reaches.
# Later sections are informational (imported but not called) and are not gated.
sed -n '/^=== Symbol Results ===/,/^Your code is affected/p' "$work/report.txt" \
  | grep -oE 'GO-[0-9]{4}-[0-9]+' | sort -u > "$work/found"
grep -oE '^GO-[0-9]{4}-[0-9]+' "$ALLOWLIST" | sort -u > "$work/allowed"

status=0

if unexpected=$(grep -vxF -f "$work/allowed" "$work/found"); then
  echo "New vulnerabilities affecting gwlint:" >&2
  echo "$unexpected" | sed 's|.*|  & (https://pkg.go.dev/vuln/&)|' >&2
  echo >&2
  echo "Upgrade the affected module if a fix exists. If none does, add the ID to" >&2
  echo "$ALLOWLIST with why it is not reachable in gwlint." >&2
  status=1
fi

if stale=$(grep -vxF -f "$work/found" "$work/allowed"); then
  echo "Allowlisted vulnerabilities no longer reported; remove them from $ALLOWLIST:" >&2
  echo "$stale" | sed 's|^|  |' >&2
  status=1
fi

if [ "$status" -eq 0 ]; then
  echo "govulncheck: no vulnerabilities outside $ALLOWLIST ($(wc -l < "$work/allowed" | tr -d ' ') allowlisted)"
fi
exit "$status"
