#!/bin/sh
# check-imports fails when a standard-library package that go-arch-lint cannot
# see is imported outside its one allowed place:
#   database/sql (and its subpackages) only in internal/store;
#   os/exec only in internal/gitsrc and the helpers under internal/testutil.
# go list without -test reports non-test imports only, so tests are not checked.
set -eu

cd "$(dirname "$0")/.."

mod=$(go list -m) || {
	echo "check-imports: go list -m failed" >&2
	exit 1
}
imports=$(go list -f '{{.ImportPath}} {{join .Imports " "}}' ./...) || {
	echo "check-imports: go list ./... failed" >&2
	exit 1
}

bad=$(printf '%s\n' "$imports" | awk -v m="$mod" '
	{
		pkg = $1
		for (i = 2; i <= NF; i++) {
			if (($i == "database/sql" || index($i, "database/sql/") == 1) && pkg != m "/internal/store")
				print pkg " imports " $i " (only internal/store may)"
			if ($i == "os/exec" && pkg != m "/internal/gitsrc" && pkg != m "/internal/testutil" && index(pkg, m "/internal/testutil/") != 1)
				print pkg " imports " $i " (only internal/gitsrc and internal/testutil may)"
		}
	}')

if [ -n "$bad" ]; then
	echo "check-imports: forbidden standard-library imports:" >&2
	printf '%s\n' "$bad" >&2
	exit 1
fi
