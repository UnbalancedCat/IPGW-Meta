#!/usr/bin/env bash
set -euo pipefail

readonly filter='scripts/candidate-checks.jq'
readonly sha='0123456789abcdef0123456789abcdef01234567'
readonly other_sha='fedcba9876543210fedcba9876543210fedcba98'
readonly -a expected=(
    'Documentation, vet, and secrets'
    'Tests (ubuntu-latest)'
    'Tests (windows-latest)'
    'Tests (macos-latest)'
    'Race detector'
    'Cross-build six supported targets'
    'Package one native-install asset batch'
    'Native install (linux-amd64, full)'
    'Native install (linux-arm64, smoke)'
    'Native install (windows-amd64, full)'
    'Native install (windows-arm64, smoke)'
    'Native install (darwin-amd64, smoke)'
    'Native install (darwin-arm64, full)'
)

base_json=$(jq -n --arg sha "$sha" --args '
  $ARGS.positional as $names |
  {
    total_count: ($names | length),
    check_runs: [
      $names | to_entries[] | {
        name: .value,
        head_sha: $sha,
        app: {id: 15368},
        started_at: "2026-09-01T00:00:00Z",
        id: (.key + 1),
        status: "completed",
        conclusion: "success"
      }
    ]
  }
' "${expected[@]}")

expect_pass() {
    local label=$1
    local json=$2
    if ! jq -e --arg sha "$sha" -f "$filter" >/dev/null <<<"$json"; then
        printf 'expected pass: %s\n' "$label" >&2
        exit 1
    fi
}

expect_blocked() {
    local label=$1
    local json=$2
    if jq -e --arg sha "$sha" -f "$filter" >/dev/null <<<"$json"; then
        printf 'expected blocked: %s\n' "$label" >&2
        exit 1
    fi
}

expect_pass base "$base_json"

for name in "${expected[@]}"; do
    candidate=$(jq --arg name "$name" '
      .check_runs |= map(select(.name != $name)) |
      .total_count = (.check_runs | length)
    ' <<<"$base_json")
    expect_blocked "missing $name" "$candidate"
done

expect_blocked status "$(jq '.check_runs[0].status = "in_progress"' <<<"$base_json")"
expect_blocked conclusion "$(jq '.check_runs[0].conclusion = "failure"' <<<"$base_json")"
expect_blocked head_sha "$(jq --arg sha "$other_sha" '.check_runs[0].head_sha = $sha' <<<"$base_json")"
expect_blocked app_id "$(jq '.check_runs[0].app.id = 1' <<<"$base_json")"
expect_blocked page_bound "$(jq '.total_count = 101' <<<"$base_json")"

older_failure=$(jq '
  .check_runs += [{
    name: .check_runs[0].name,
    head_sha: .check_runs[0].head_sha,
    app: {id: 15368},
    started_at: "2026-08-31T23:59:59Z",
    id: 0,
    status: "completed",
    conclusion: "failure"
  }] |
  .total_count = (.check_runs | length)
' <<<"$base_json")
expect_pass older_failure_ignored "$older_failure"

newer_failure=$(jq '
  .check_runs += [{
    name: .check_runs[0].name,
    head_sha: .check_runs[0].head_sha,
    app: {id: 15368},
    started_at: "2026-09-01T00:00:01Z",
    id: 999,
    status: "completed",
    conclusion: "failure"
  }] |
  .total_count = (.check_runs | length)
' <<<"$base_json")
expect_blocked newer_failure_wins "$newer_failure"

printf 'candidate check-run gate tests passed (%d cases)\n' "$((1 + ${#expected[@]} + 7))"
