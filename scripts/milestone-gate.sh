#!/usr/bin/env bash
set -euo pipefail

usage() {
    printf 'usage: milestone-gate.sh candidate|promotion|release STATUS_FILE\n' >&2
    exit 2
}

if [ "$#" -ne 2 ]; then
    usage
fi

mode=$1
status_file=$2

case "$mode" in
    candidate|promotion|release) ;;
    *) usage ;;
esac

if [ ! -f "$status_file" ] || [ -L "$status_file" ]; then
    printf '%s blocked: status must be a regular non-symlink file\n' "$mode" >&2
    exit 1
fi

require_milestone() {
    milestone=$1
    expected=$2
    rows=$(grep -Ec "^\\| $milestone [^|]*\\| [^|]+ \\|" -- "$status_file" || :)
    matching=$(grep -Ec "^\\| $milestone [^|]*\\| $expected \\|" -- "$status_file" || :)
    if [ "$rows" -ne 1 ] || [ "$matching" -ne 1 ]; then
        printf '%s blocked: %s must appear exactly once with status %s\n' \
            "$mode" "$milestone" "$expected" >&2
        exit 1
    fi
}

require_milestone M1 complete
require_milestone M2 complete

case "$mode" in
    candidate)
        ;;
    promotion)
        require_milestone M3 in_progress
        ;;
    release)
        require_milestone M3 complete
        ;;
esac
