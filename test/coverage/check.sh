#!/usr/bin/env bash
set -euo pipefail

profile="${1:-coverage/coverage.out}"
if [[ ! -s "$profile" ]]; then
  echo "coverage profile not found: $profile" >&2
  exit 1
fi

# Package-specific floors concentrate the gate on allocation, isolation,
# recovery, health, and topology code. Duplicate coverpkg blocks are merged by
# source range so packages exercised by several test binaries are counted once.
selectors=(
  all
  /src/internal/controller/
  /src/internal/device/
  /src/internal/dra/
  /src/internal/lifecycle/
  /src/internal/observability/
  /src/internal/placement/
  /src/internal/topology/
)
names=(overall controller device dra lifecycle observability placement topology)
minimums=(70.0 80.0 70.0 95.0 69.0 75.0 90.0 90.0)

failed=0
for index in "${!selectors[@]}"; do
  coverage="$({
    awk -v selector="${selectors[$index]}" '
      NR == 1 { next }
      selector != "all" && index($1, selector) == 0 { next }
      {
        key = $1
        statements[key] = $2
        if ($3 > 0) {
          covered[key] = $2
        }
      }
      END {
        total = hit = 0
        for (key in statements) {
          total += statements[key]
          hit += covered[key]
        }
        if (total == 0) {
          print "0.0"
        } else {
          printf "%.1f\n", 100 * hit / total
        }
      }
    ' "$profile"
  })"
  printf '%-16s %6s%% (minimum %s%%)\n' "${names[$index]}" "$coverage" "${minimums[$index]}"
  if ! awk -v actual="$coverage" -v minimum="${minimums[$index]}" 'BEGIN { exit !(actual >= minimum) }'; then
    failed=1
  fi
done

if ((failed)); then
  echo "risk-based coverage gate failed" >&2
  exit 1
fi
