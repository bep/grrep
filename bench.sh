#!/bin/bash

LITERAL=_SUSPEND
REGEX='[A-Z]+_SUSPEND'
ROOT=/Users/bep/dev/dump/linux
N=${N:-5}

go build -o /tmp/mygrep . || exit 1

export TIMEFORMAT='%R'

median() {
	local times=()
	for ((i = 0; i < N; i++)); do
		t=$({ time "$@" >/dev/null 2>&1; } 2>&1)
		times+=("$t")
	done
	printf '%s\n' "${times[@]}" | sort -n | awk -v n=$N '
		{ a[NR] = $1 }
		END {
			m = (n%2) ? a[(n+1)/2] : (a[n/2] + a[n/2+1]) / 2
			printf "%.3f\n", m
		}
	'
}

ratio() {
	awk -v a="$1" -v b="$2" 'BEGIN { if (b > 0) printf "%.2fx", a/b }'
}

echo "$ROOT  (literal: $LITERAL, regex: $REGEX)"
echo "n=$N iterations per variant; reporting median wall time"

rg_lq=$(median rg -q "$LITERAL" "$ROOT")
rg_lf=$(median rg "$LITERAL" "$ROOT")
my_lq=$(median /tmp/mygrep -q "$LITERAL" "$ROOT")
my_lf=$(median /tmp/mygrep "$LITERAL" "$ROOT")
rg_rq=$(median rg -q "$REGEX" "$ROOT")
rg_rf=$(median rg "$REGEX" "$ROOT")
my_rq=$(median /tmp/mygrep -q "$REGEX" "$ROOT")
my_rf=$(median /tmp/mygrep "$REGEX" "$ROOT")

printf '\n'
printf '%-15s %10s %10s %8s\n' "variant"         "rg"         "mygrep"     "ratio"
printf '%-15s %10s %10s %8s\n' "---------------" "----------" "----------" "--------"
printf '%-15s %10s %10s %8s\n' "literal full"    "${rg_lf}s"  "${my_lf}s"  "$(ratio "$rg_lf" "$my_lf")"
printf '%-15s %10s %10s %8s\n' "regex   full"    "${rg_rf}s"  "${my_rf}s"  "$(ratio "$rg_rf" "$my_rf")"
printf '%-15s %10s %10s %8s\n' "literal quiet"   "${rg_lq}s"  "${my_lq}s"  ""
printf '%-15s %10s %10s %8s\n' "regex   quiet"   "${rg_rq}s"  "${my_rq}s"  ""
