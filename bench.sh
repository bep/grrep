#!/bin/bash

LITERAL=_SUSPEND
REGEX='[A-Z]+_SUSPEND'
ROOT=/Users/bep/dev/dump/linux
N=${N:-5}

go build -o /tmp/mygrep . || exit 1

export TIMEFORMAT='%R'

bench() {
	local label="$1"
	shift
	local times=()
	for ((i = 0; i < N; i++)); do
		t=$({ time "$@" >/dev/null 2>&1; } 2>&1)
		times+=("$t")
	done
	printf '%s\n' "${times[@]}" | sort -n | awk -v label="$label" -v n=$N '
		{ a[NR] = $1 }
		END {
			med = (n%2) ? a[(n+1)/2] : (a[n/2] + a[n/2+1]) / 2
			printf "%s median=%.3fs  min=%.3fs  max=%.3fs\n", label, med, a[1], a[n]
		}
	'
}

echo "n=$N iterations per variant"

echo "--- literal: $LITERAL ---"
bench 'ripgrep	quiet:' rg -q "$LITERAL" "$ROOT"
bench 'ripgrep	full: ' rg "$LITERAL" "$ROOT"
bench 'mygrep	quiet:' /tmp/mygrep -q "$LITERAL" "$ROOT"
bench 'mygrep	full: ' /tmp/mygrep "$LITERAL" "$ROOT"

echo "--- regex:   $REGEX ---"
bench 'ripgrep	quiet:' rg -q "$REGEX" "$ROOT"
bench 'ripgrep	full: ' rg "$REGEX" "$ROOT"
bench 'mygrep	quiet:' /tmp/mygrep -q "$REGEX" "$ROOT"
bench 'mygrep	full: ' /tmp/mygrep "$REGEX" "$ROOT"
