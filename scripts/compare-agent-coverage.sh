#!/usr/bin/env bash
# Compare agent package test coverage between current branch and main.
# Usage: ./scripts/compare-agent-coverage.sh
set -euo pipefail

CURRENT_BRANCH=$(git branch --show-current)
PKG="./internal/agent/"

echo "=== Capturing main coverage ==="
git checkout origin/main
go clean -testcache
go test -coverprofile=/tmp/main-agent-cov.out "$PKG"
echo "Main total:"
go tool cover -func=/tmp/main-agent-cov.out | tail -1

echo ""
echo "=== Capturing $CURRENT_BRANCH coverage ==="
git checkout "$CURRENT_BRANCH"
go clean -testcache
go test -coverprofile=/tmp/branch-agent-cov.out "$PKG"
echo "Branch total:"
go tool cover -func=/tmp/branch-agent-cov.out | tail -1

echo ""
echo "=== Functions that lost coverage (main > 0%, branch = 0%) ==="
comm -23 \
  <(go tool cover -func=/tmp/main-agent-cov.out | grep -v "0.0%" | awk '{print $1, $NF}' | sort) \
  <(go tool cover -func=/tmp/branch-agent-cov.out | grep -v "0.0%" | awk '{print $1, $NF}' | sort)

echo ""
echo "=== New functions with coverage (branch > 0%, main = 0%) ==="
comm -13 \
  <(go tool cover -func=/tmp/main-agent-cov.out | grep -v "0.0%" | awk '{print $1, $NF}' | sort) \
  <(go tool cover -func=/tmp/branch-agent-cov.out | grep -v "0.0%" | awk '{print $1, $NF}' | sort)

echo ""
echo "=== Per-function diff on modified files ==="
for file in agent_run coordinator background loop sandboxed_runner sandbox exec; do
  echo "--- $file.go ---"
  diff <(go tool cover -func=/tmp/main-agent-cov.out | grep "$file\.go:" | awk '{print $1, $NF}' | sort) \
       <(go tool cover -func=/tmp/branch-agent-cov.out | grep "$file\.go:" | awk '{print $1, $NF}' | sort) || true
done
