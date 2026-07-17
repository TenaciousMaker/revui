#!/usr/bin/env sh
set -eu

minimum=75
profile="${TMPDIR:-/tmp}/revui-coverage.out"
go test -coverprofile="$profile" ./...
coverage="$(go tool cover -func="$profile" | awk '/^total:/ { gsub("%", "", $3); print $3 }')"
awk -v actual="$coverage" -v minimum="$minimum" 'BEGIN {
  if (actual + 0 < minimum) {
    printf "coverage %.1f%% is below required %d%%\n", actual, minimum
    exit 1
  }
  printf "coverage %.1f%% meets required %d%%\n", actual, minimum
}'
