#!/bin/bash
set -euo pipefail

REGEX='[A-Z]+_SUSPEND'
ROOT=/Volumes/LinuxBench/linux
N=${N:-5}

go build -o /tmp/mygrep . || exit 1

export TIMEFORMAT='%R'

median() {
	local times=()
	for ((i = 0; i < N; i++)); do
		t=$({ time "$@" 2>/dev/null | cat >/dev/null; } 2>&1)
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

matches() {
	"$@" 2>/dev/null | wc -l | tr -d ' '
}

echo "$ROOT  (regex: $REGEX)"
echo "n=$N iterations per variant; reporting median wall time"

RG=(rg "$REGEX" "$ROOT")
MY=(/tmp/mygrep "$REGEX" "$ROOT")
UG=(ugrep -r --ignore-files --no-hidden -I "$REGEX" "$ROOT")

rg_rf=$(median "${RG[@]}")
my_rf=$(median "${MY[@]}")
ug_rf=$(median "${UG[@]}")

rg_n=$(matches "${RG[@]}")
my_n=$(matches "${MY[@]}")
ug_n=$(matches "${UG[@]}")

printf '\n'
printf '%10s %10s %10s %8s\n' "tool" "time" "matches" "ratio"
printf '%10s %10s %10s %8s\n' "----------" "----------" "----------" "--------"
printf '%10s %10s %10s %8s\n' "mygrep" "${my_rf}s" "$my_n" "$(ratio "$my_rf" "$my_rf")"
printf '%10s %10s %10s %8s\n' "ripgrep" "${rg_rf}s" "$rg_n" "$(ratio "$rg_rf" "$my_rf")"
printf '%10s %10s %10s %8s\n' "ugrep" "${ug_rf}s" "$ug_n" "$(ratio "$ug_rf" "$my_rf")"
