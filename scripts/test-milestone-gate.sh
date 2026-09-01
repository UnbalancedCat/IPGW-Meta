#!/usr/bin/env bash
set -euo pipefail

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
gate=$root/scripts/milestone-gate.sh
stage=$(mktemp -d "${TMPDIR:-/tmp}/ipgw-meta-milestone-gate.XXXXXX")
trap 'rm -rf -- "$stage"' EXIT HUP INT TERM

case_count=0

write_status() {
    path=$1
    m0=$2
    m1=$3
    m2=$4
    m3=$5
    {
        printf '%s\n' '| 里程碑 | 状态 | 发布门禁 |'
        printf '%s\n' '|---|---|---|'
        if [ "$m0" != absent ]; then
            printf '| M0 文档与紧急安全 | %s | residual governance |\n' "$m0"
        fi
        if [ "$m1" != absent ]; then
            printf '| M1 协议正确性与 SDK | %s | protocol |\n' "$m1"
        fi
        if [ "$m2" != absent ]; then
            printf '| M2 三入口、配置与自动化 | %s | packaging |\n' "$m2"
        fi
        if [ "$m3" != absent ]; then
            printf '| M3 v1 候选与稳定发布 | %s | release |\n' "$m3"
        fi
    } >"$path"
}

expect_pass() {
    mode=$1
    path=$2
    case_count=$((case_count + 1))
    if ! "$BASH" "$gate" "$mode" "$path" >"$stage/stdout" 2>"$stage/stderr"; then
        printf 'expected pass: mode=%s case=%s\n' "$mode" "$case_count" >&2
        sed 's/^/gate stderr: /' "$stage/stderr" >&2
        "$BASH" -x "$gate" "$mode" "$path" >&2 || :
        exit 1
    fi
    if [ -s "$stage/stdout" ] || [ -s "$stage/stderr" ]; then
        printf 'passing gate polluted output: mode=%s case=%s\n' "$mode" "$case_count" >&2
        exit 1
    fi
}

expect_blocked() {
    mode=$1
    path=$2
    case_count=$((case_count + 1))
    set +e
    "$BASH" "$gate" "$mode" "$path" >"$stage/stdout" 2>"$stage/stderr"
    code=$?
    set -e
    if [ "$code" -ne 1 ] || [ -s "$stage/stdout" ] || [ ! -s "$stage/stderr" ]; then
        printf 'expected closed failure: mode=%s case=%s code=%s\n' \
            "$mode" "$case_count" "$code" >&2
        exit 1
    fi
}

for m0 in absent not_started in_progress blocked complete; do
    write_status "$stage/candidate-$m0.md" "$m0" complete complete absent
    expect_pass candidate "$stage/candidate-$m0.md"

    write_status "$stage/promotion-$m0.md" "$m0" complete complete in_progress
    expect_pass promotion "$stage/promotion-$m0.md"

    write_status "$stage/release-$m0.md" "$m0" complete complete complete
    expect_pass release "$stage/release-$m0.md"
done

for mode in candidate promotion release; do
    if [ "$mode" = release ]; then
        m3=complete
    else
        m3=in_progress
    fi
    for m1 in absent not_started in_progress blocked; do
        write_status "$stage/$mode-m1-$m1.md" in_progress "$m1" complete "$m3"
        expect_blocked "$mode" "$stage/$mode-m1-$m1.md"
    done
    for m2 in absent not_started in_progress blocked; do
        write_status "$stage/$mode-m2-$m2.md" in_progress complete "$m2" "$m3"
        expect_blocked "$mode" "$stage/$mode-m2-$m2.md"
    done
done

write_status "$stage/duplicate.md" in_progress complete complete in_progress
printf '%s\n' '| M1 duplicate | complete | protocol |' >>"$stage/duplicate.md"
expect_blocked candidate "$stage/duplicate.md"

write_status "$stage/duplicate-m2.md" in_progress complete complete in_progress
printf '%s\n' '| M2 duplicate | complete | packaging |' >>"$stage/duplicate-m2.md"
expect_blocked candidate "$stage/duplicate-m2.md"

for m3 in absent not_started blocked complete; do
    write_status "$stage/promotion-m3-$m3.md" in_progress complete complete "$m3"
    expect_blocked promotion "$stage/promotion-m3-$m3.md"
done
write_status "$stage/promotion-duplicate.md" in_progress complete complete in_progress
printf '%s\n' '| M3 duplicate | in_progress | release |' >>"$stage/promotion-duplicate.md"
expect_blocked promotion "$stage/promotion-duplicate.md"

for m3 in absent not_started in_progress blocked; do
    write_status "$stage/release-m3-$m3.md" in_progress complete complete "$m3"
    expect_blocked release "$stage/release-m3-$m3.md"
done
write_status "$stage/release-duplicate.md" in_progress complete complete complete
printf '%s\n' '| M3 duplicate | complete | release |' >>"$stage/release-duplicate.md"
expect_blocked release "$stage/release-duplicate.md"

expect_blocked candidate "$stage/missing.md"
write_status "$stage/regular.md" in_progress complete complete in_progress
if ln -s "regular.md" "$stage/status-link.md" 2>/dev/null && [ -L "$stage/status-link.md" ]; then
    expect_blocked candidate "$stage/status-link.md"
fi

set +e
"$BASH" "$gate" unknown "$stage/regular.md" >"$stage/stdout" 2>"$stage/stderr"
code=$?
set -e
case_count=$((case_count + 1))
if [ "$code" -ne 2 ] || [ -s "$stage/stdout" ] || [ ! -s "$stage/stderr" ]; then
    printf 'invalid mode did not return usage failure\n' >&2
    exit 1
fi

printf 'milestone gate tests: ok (%s cases)\n' "$case_count"
