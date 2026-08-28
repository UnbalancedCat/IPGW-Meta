#!/usr/bin/env bash
set -euo pipefail
umask 077

repository_root=$(cd "$(dirname "$0")/.." && pwd -P)
config_file="$repository_root/.gitleaks.toml"
gitleaks_bin=${GITLEAKS_BIN:-}
if [[ -z $gitleaks_bin ]]; then
    if command -v gitleaks >/dev/null 2>&1; then
        gitleaks_bin=$(command -v gitleaks)
    elif command -v go >/dev/null 2>&1; then
        candidate="$(go env GOPATH)/bin/gitleaks"
        if [[ -x $candidate ]]; then
            gitleaks_bin=$candidate
        fi
    fi
fi
if [[ -z $gitleaks_bin || ! -x $gitleaks_bin ]]; then
    echo "gitleaks is required to test secret detection rules" >&2
    exit 2
fi

if command -v sha256sum >/dev/null 2>&1; then
    hash_canary() { printf 'ipgw-meta-gitleaks-canary-%s' "$1" | sha256sum | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
    hash_canary() { printf 'ipgw-meta-gitleaks-canary-%s' "$1" | shasum -a 256 | awk '{print $1}'; }
else
    echo "sha256sum or shasum is required to generate synthetic canaries" >&2
    exit 2
fi

temporary_base=${TMPDIR:-/tmp}
temporary_base=$(cd "$temporary_base" && pwd -P)
temporary_dir=$(mktemp -d "$temporary_base/ipgw-meta-gitleaks.XXXXXX")
cleanup() {
    case "$temporary_dir" in
        "$temporary_base"/ipgw-meta-gitleaks.*)
            rm -rf -- "$temporary_dir"
            ;;
        *)
            echo "refusing to remove unexpected temporary path" >&2
            ;;
    esac
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

positive_dir="$temporary_dir/positive"
negative_dir="$temporary_dir/negative"
mkdir -p "$positive_dir" "$negative_dir"
positive_file="$positive_dir/canaries.txt"
negative_file="$negative_dir/documentation-examples.txt"
positive_report="$temporary_dir/positive-report.json"
negative_report="$temporary_dir/negative-report.json"
positive_log="$temporary_dir/positive.log"
negative_log="$temporary_dir/negative.log"

st_value=$(hash_canary st)
tgt_value=$(hash_canary tgt)
session_value=$(hash_canary session)
php_session_value=$(hash_canary php-session)
cookie_csrf_value=$(hash_canary cookie-csrf)
header_csrf_value=$(hash_canary header-csrf)
query_csrf_value=$(hash_canary query-csrf)
ticket_value=$(hash_canary ticket-query)

{
    printf 'cas_result = "ST-1-%s"\n' "$st_value"
    printf 'tgt_result = "TGT-1-%s"\n' "$tgt_value"
    printf 'Cookie: mysession=%s\n' "$session_value"
    printf 'Cookie: phpsessid_8800=%s\n' "$php_session_value"
    printf 'Cookie: csrf-8800=%s\n' "$cookie_csrf_value"
    printf 'Header.Set("X-CSRF-Token", "%s")\n' "$header_csrf_value"
    printf 'https://portal.invalid/path?csrf_token=%s\n' "$query_csrf_value"
    printf 'https://portal.invalid/path?ticket=%s\n' "$ticket_value"
} >"$positive_file"

cat >"$negative_file" <<'EOF'
Documentation may discuss ST/TGT, ticket query handling, the mysession Cookie,
and X-CSRF-Token without containing authentication material.
ticket=<redacted>
mysession=<redacted>
csrf-8800=<redacted>
X-CSRF-Token
ST-EXAMPLE
TGT-EXAMPLE
csrf_token=placeholder
EOF

set +e
"$gitleaks_bin" dir \
    --config="$config_file" \
    --redact \
    --no-banner \
    --report-format=json \
    --report-path="$positive_report" \
    "$positive_dir" >"$positive_log" 2>&1
positive_status=$?
set -e
if [[ $positive_status -ne 1 || ! -f $positive_report ]]; then
    echo "positive gitleaks canaries did not produce the expected redacted findings" >&2
    exit 1
fi

for rule_id in \
    neu-cas-service-ticket \
    neu-cas-ticket-granting-ticket \
    neu-campus-session-cookie \
    neu-csrf-token-material \
    neu-ticket-query-material
do
    if ! grep -Eq '"RuleID"[[:space:]]*:[[:space:]]*"'"$rule_id"'"' "$positive_report"; then
        echo "expected gitleaks rule did not match: $rule_id" >&2
        exit 1
    fi
done

set +e
"$gitleaks_bin" dir \
    --config="$config_file" \
    --redact \
    --no-banner \
    --report-format=json \
    --report-path="$negative_report" \
    "$negative_dir" >"$negative_log" 2>&1
negative_status=$?
set -e
if [[ $negative_status -ne 0 ]]; then
    echo "documentation-only negative canaries produced a finding" >&2
    exit 1
fi

echo "gitleaks synthetic canaries passed"
