#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C

repo_url=""
install_root=${IPGW_INSTALL_ROOT:-/usr/local/lib/ipgw-meta}
bin_dir=${IPGW_BIN_DIR:-/usr/local/bin}
bundle_path=""
bundle_sha256=""
expected_version=${IPGW_VERSION:-}

require_value() {
    local option=$1
    local value=${2:-}
    if [[ -z $value ]]; then
        echo "$option requires a value" >&2
        exit 2
    fi
}

while (($# > 0)); do
    case "$1" in
        --bundle)
            require_value "$1" "${2:-}"
            bundle_path=$2
            shift 2
            ;;
        --bundle-sha256)
            require_value "$1" "${2:-}"
            bundle_sha256=$2
            shift 2
            ;;
        --version)
            require_value "$1" "${2:-}"
            if [[ -n ${IPGW_VERSION:-} && $2 != "$IPGW_VERSION" ]]; then
                echo "--version does not match the pinned installer version" >&2
                exit 2
            fi
            expected_version=$2
            shift 2
            ;;
        --install-root)
            require_value "$1" "${2:-}"
            install_root=$2
            shift 2
            ;;
        --bin-dir)
            require_value "$1" "${2:-}"
            bin_dir=$2
            shift 2
            ;;
        --help)
            cat <<'EOF'
usage: install.sh [--bundle ABS_ARCHIVE --bundle-sha256 HEX64]
                  [--version EXPECTED] [--install-root ABS_PATH]
                  [--bin-dir ABS_PATH]
EOF
            exit 0
            ;;
        *)
            echo "unknown installer argument: $1" >&2
            exit 2
            ;;
    esac
done

offline=0
if [[ -n $bundle_path || -n $bundle_sha256 ]]; then
    if [[ -z $bundle_path || -z $bundle_sha256 ]]; then
        echo "--bundle and --bundle-sha256 must be provided together" >&2
        exit 2
    fi
    offline=1
fi
if [[ -n $bundle_sha256 && ! $bundle_sha256 =~ ^[0-9A-Fa-f]{64}$ ]]; then
    echo "--bundle-sha256 must be exactly 64 hexadecimal characters" >&2
    exit 2
fi
if [[ -n $expected_version && ! $expected_version =~ ^v?[0-9A-Za-z][0-9A-Za-z._+-]*$ ]]; then
    echo "invalid expected version" >&2
    exit 2
fi

kernel=$(uname -s)
machine=$(uname -m)
case "$kernel" in
    Darwin) goos=darwin ;;
    Linux) goos=linux ;;
    *) echo "unsupported operating system: $kernel" >&2; exit 2 ;;
esac
case "$machine" in
    x86_64|amd64) goarch=amd64 ;;
    arm64|aarch64) goarch=arm64 ;;
    *) echo "unsupported architecture: $machine" >&2; exit 2 ;;
esac

target="$goos-$goarch"
archive_name="ipgw-meta-$target.tar.gz"
release_base=""
if ((offline == 0)); then
    repo_url="https://github.com/UnbalancedCat/ipgw-meta"
    if [[ -n $expected_version ]]; then
        release_base="$repo_url/releases/download/$expected_version"
    else
        release_base="$repo_url/releases/latest/download"
    fi
    if ! command -v curl >/dev/null 2>&1; then
        echo "curl is required to install IPGW-Meta" >&2
        exit 2
    fi
fi
if ! command -v tar >/dev/null 2>&1; then
    echo "tar is required to install IPGW-Meta" >&2
    exit 2
fi
if ! command -v cmp >/dev/null 2>&1; then
    echo "cmp is required to install IPGW-Meta" >&2
    exit 2
fi

if command -v sha256sum >/dev/null 2>&1; then
    hash_file() { sha256sum "$1" | awk '{print tolower($1)}'; }
elif command -v shasum >/dev/null 2>&1; then
    hash_file() { shasum -a 256 "$1" | awk '{print tolower($1)}'; }
else
    echo "sha256sum or shasum is required to install IPGW-Meta" >&2
    exit 2
fi

size_file() {
    wc -c <"$1" | tr -d '[:space:]'
}

lower_hex() {
    printf '%s' "$1" | tr 'A-F' 'a-f'
}

stat_fields() {
    case "$kernel" in
        Darwin) stat -Lf '%Lp %z %u %d %i' -- "$1" ;;
        Linux) stat -Lc '%a %s %u %d %i' -- "$1" ;;
    esac
}

stat_open_bundle_fields() {
    case "$kernel" in
        Darwin) stat -f '%Lp %z %u %d %i' <&9 ;;
        Linux) stat -Lc '%a %s %u %d %i' -- /dev/fd/9 ;;
    esac
}

validate_clean_absolute_path() {
    local path=$1
    local label=$2
    local component
    local -a components
    if [[ $path != /* || $path == / || $path == */ || $path == *//* || $path =~ [[:cntrl:]] ]]; then
        echo "$label must be an absolute, clean, non-root path" >&2
        exit 2
    fi
    IFS='/' read -r -a components <<<"${path#/}"
    for component in "${components[@]}"; do
        if [[ -z $component || $component == . || $component == .. ]]; then
            echo "$label contains an unsafe path component" >&2
            exit 2
        fi
    done
}

validate_directory_ancestors() {
    local path=$1
    local label=$2
    local current=""
    local component
    local -a components
    IFS='/' read -r -a components <<<"${path#/}"
    for component in "${components[@]}"; do
        current="$current/$component"
        if [[ -L $current ]]; then
            echo "$label contains a symbolic-link component" >&2
            exit 1
        fi
        if [[ -e $current && ! -d $current ]]; then
            echo "$label contains a non-directory component" >&2
            exit 1
        fi
    done
}

path_is_within() {
    local child=$1
    local parent=$2
    [[ $child != "$parent" && $child == "$parent"/* ]]
}

paths_overlap() {
    local left=$1
    local right=$2
    [[ $left == "$right" || $left == "$right"/* || $right == "$left"/* ]]
}

validate_failpoint() {
    local value=$1
    local allowed=$2
    local label=$3
    if [[ -n $value ]]; then
        case "|$allowed|" in
            *"|$value|"*) ;;
            *) echo "invalid $label" >&2; exit 2 ;;
        esac
    fi
}

if [[ -z ${HOME:-} || $HOME != /* ]]; then
    echo "HOME must be an absolute path" >&2
    exit 2
fi
if [[ $kernel == Darwin ]]; then
    config_base="$HOME/Library/Application Support"
else
    config_base=${XDG_CONFIG_HOME:-$HOME/.config}
fi
launcher_dir="$config_base/ipgw-meta"
launcher_file="$launcher_dir/launcher.yaml"

validate_clean_absolute_path "$install_root" "install root"
validate_clean_absolute_path "$bin_dir" "binary directory"
validate_clean_absolute_path "$config_base" "configuration directory"
validate_clean_absolute_path "$launcher_dir" "launcher directory"
validate_directory_ancestors "$install_root" "install root"
validate_directory_ancestors "$bin_dir" "binary directory"
validate_directory_ancestors "$config_base" "configuration directory"
if paths_overlap "$install_root" "$bin_dir" || paths_overlap "$install_root" "$launcher_dir" || paths_overlap "$bin_dir" "$launcher_dir"; then
    echo "install root, binary directory, and launcher directory must not overlap" >&2
    exit 2
fi
if [[ $install_root == "$HOME" || $bin_dir == "$HOME" || $launcher_dir == "$HOME" ]]; then
    echo "refusing to use the user home directory as an installation target" >&2
    exit 2
fi
script_dir=$(cd "$(dirname "$0")" && pwd -P)
if [[ -d $script_dir/.git ]]; then
    if paths_overlap "$install_root" "$script_dir" || paths_overlap "$bin_dir" "$script_dir" || paths_overlap "$launcher_dir" "$script_dir"; then
        echo "refusing an installation target that overlaps the repository" >&2
        exit 2
    fi
fi

test_root=${IPGW_INSTALL_TEST_ROOT:-}
test_token=${IPGW_INSTALL_TEST_TOKEN:-}
test_failpoint=${IPGW_INSTALL_TEST_FAILPOINT:-}
test_rollback_failpoint=${IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT:-}
test_mode=0
if [[ -n $test_root || -n $test_token || -n $test_failpoint || -n $test_rollback_failpoint ]]; then
    if ((offline == 0)); then
        echo "installer test controls require offline mode" >&2
        exit 2
    fi
    if [[ -z $test_root || -z $test_token || ${#test_token} -gt 128 || $test_token =~ [^0-9A-Za-z._+-] ]]; then
        echo "installer test root and safe token are required" >&2
        exit 2
    fi
    validate_clean_absolute_path "$test_root" "installer test root"
    validate_directory_ancestors "$test_root" "installer test root"
    if [[ -L $test_root || ! -d $test_root ]]; then
        echo "installer test root must already be a real directory created by the test run" >&2
        exit 2
    fi
    read -r root_mode _ root_uid _ _ <<<"$(stat_fields "$test_root")"
    if (( (8#$root_mode & 0077) != 0 )) || [[ $root_uid != "$(id -u)" ]]; then
        echo "installer test root must be private and owned by the current user" >&2
        exit 2
    fi
    token_file="$test_root/.ipgw-install-test-token"
    if [[ -L $token_file || ! -f $token_file ]]; then
        echo "installer test token file is missing or unsafe" >&2
        exit 2
    fi
    read -r token_mode token_size token_uid _ _ <<<"$(stat_fields "$token_file")"
    if ((token_size < 1 || token_size > 128 || (8#$token_mode & 0077) != 0)) || [[ $token_uid != "$(id -u)" ]]; then
        echo "installer test token file is not private" >&2
        exit 2
    fi
    if [[ $(cat -- "$token_file") != "$test_token" ]]; then
        echo "installer test token does not match" >&2
        exit 2
    fi
    validate_failpoint "$test_failpoint" 'after_verified_stage|after_version_publish|after_old_active_detach|after_active_switch|after_entry_1|after_entry_2|after_launcher_publish|after_path_update|before_commit' 'installer failpoint'
    validate_failpoint "$test_rollback_failpoint" 'before_restore_active|before_restore_entry_1|before_remove_new_version' 'installer rollback failpoint'
    for test_path in "$bundle_path" "$install_root" "$bin_dir" "$launcher_dir"; do
        if ! path_is_within "$test_path" "$test_root"; then
            echo "all installer test inputs and targets must be inside the private test root" >&2
            exit 2
        fi
    done
    test_mode=1
fi

bundle_fd_open=0
if ((offline)); then
    validate_clean_absolute_path "$bundle_path" "bundle path"
    bundle_parent=${bundle_path%/*}
    if [[ -z $bundle_parent ]]; then bundle_parent=/; fi
    if [[ $bundle_parent != / ]]; then validate_directory_ancestors "$bundle_parent" "bundle path"; fi
    if [[ -L $bundle_path || ! -f $bundle_path ]]; then
        echo "offline bundle must be a non-symbolic regular file" >&2
        exit 2
    fi
    exec 9<"$bundle_path"
    bundle_fd_open=1
    path_fields=$(stat_fields "$bundle_path")
    handle_fields=$(stat_open_bundle_fields)
    if [[ $path_fields != "$handle_fields" ]]; then
        echo "offline bundle changed while being opened" >&2
        exit 1
    fi
    read -r bundle_mode bundle_size _ _ _ <<EOF
$handle_fields
EOF
    if ((bundle_size < 1 || bundle_size > 104857600)); then
        echo "offline bundle size must be between 1 byte and 100 MiB" >&2
        exit 2
    fi
    if (( (8#$bundle_mode & 0022) != 0 )); then
        echo "offline bundle must not be group- or world-writable" >&2
        exit 2
    fi
fi

ensure_real_directory() {
    local path=$1
    local mode=${2:-0755}
    local existed=0
    if [[ -L $path ]]; then
        echo "refusing to use a symbolic-link directory: $path" >&2
        exit 1
    fi
    if [[ -e $path && ! -d $path ]]; then
        echo "refusing to use a non-directory path: $path" >&2
        exit 1
    fi
    if [[ -d $path ]]; then existed=1; fi
    mkdir -p "$path"
    if [[ -L $path || ! -d $path ]]; then
        echo "failed to create a real directory: $path" >&2
        exit 1
    fi
    if ((existed == 0)); then chmod "$mode" "$path"; fi
}

if ((test_mode)); then
    temp_parent="$test_root/.installer-tmp"
    ensure_real_directory "$temp_parent" 0700
else
    temp_parent=${TMPDIR:-/tmp}
    validate_clean_absolute_path "$temp_parent" "temporary directory"
    validate_directory_ancestors "$temp_parent" "temporary directory"
    if [[ -L $temp_parent || ! -d $temp_parent ]]; then
        echo "temporary directory must be an existing real directory" >&2
        exit 1
    fi
fi
download_dir=$(mktemp -d "$temp_parent/ipgw-meta-acquire.XXXXXX")
chmod 0700 "$download_dir"
archive_file="$download_dir/$archive_name"
checksums_file="$download_dir/SHA256SUMS"
stage=""
active_next=""
entry_next=""
backup_dir=""
transaction_dir=""
journal_file=""
version_dir=""
version_installed=0
launcher_tmp=""
launcher_installed=0
active_switched=0
active_detached=0
committed=0
had_active=0
old_active=""
new_active_target=""
rollback_failed=0
declare -a installed_entries=()
declare -a backed_entries=()

write_journal() {
    local phase=$1
    local journal_next="$transaction_dir/.journal.next.$$"
    local version_name=""
    if [[ -n $version_dir ]]; then version_name=${version_dir##*/}; fi
    local backup_name=""
    if [[ -n $backup_dir ]]; then backup_name=${backup_dir##*/}; fi
    printf 'schema_version=1\nphase=%s\nversion_name=%s\nhad_active=%s\nentry_count=%s\nbackup_name=%s\n' \
        "$phase" "$version_name" "$had_active" "${#installed_entries[@]}" "$backup_name" >"$journal_next"
    chmod 0600 "$journal_next"
    mv "$journal_next" "$journal_file"
}

maybe_fail() {
    local point=$1
    if ((test_mode)) && [[ $test_failpoint == "$point" ]]; then
        echo "installer test failpoint triggered: $point" >&2
        return 1
    fi
}

rollback_failpoint_matches() {
    local point=$1
    ((test_mode)) && [[ $test_rollback_failpoint == "$point" ]]
}

journal_records_commit() {
    local line
    if [[ -z $journal_file || -L $journal_file || ! -f $journal_file ]]; then
        return 1
    fi
    if [[ $(size_file "$journal_file") -gt 1024 ]]; then
        return 1
    fi
    while IFS= read -r line; do
        if [[ $line == phase=committed ]]; then return 0; fi
    done <"$journal_file"
    return 1
}

rollback() {
    local name target index
    local failed=0
    set +e
    if ((launcher_installed)); then
        if [[ -f $launcher_file && ! -L $launcher_file ]]; then
            rm -f -- "$launcher_file" || failed=1
        else
            failed=1
        fi
    fi

    if ((${#installed_entries[@]} > 0)); then
        if rollback_failpoint_matches before_restore_entry_1; then
            echo "installer rollback failpoint triggered: before_restore_entry_1" >&2
            failed=1
        else
            for ((index=${#installed_entries[@]} - 1; index >= 0; index--)); do
                name=${installed_entries[index]}
                target="$bin_dir/$name"
                if [[ -L $target && $(readlink "$target") == "$active_path/$name" ]]; then
                    rm -f -- "$target" || failed=1
                elif [[ -e $target || -L $target ]]; then
                    failed=1
                fi
            done
            for ((index=${#backed_entries[@]} - 1; index >= 0; index--)); do
                name=${backed_entries[index]}
                target="$bin_dir/$name"
                if [[ -e $target || -L $target || ! -e $backup_dir/$name && ! -L $backup_dir/$name ]]; then
                    failed=1
                else
                    mv "$backup_dir/$name" "$target" || failed=1
                fi
            done
        fi
    fi

    if ((failed == 0)) && ((active_switched || active_detached)); then
        if rollback_failpoint_matches before_restore_active; then
            echo "installer rollback failpoint triggered: before_restore_active" >&2
            failed=1
        else
            if ((active_switched)); then
                if [[ -L $active_path && $(readlink "$active_path") == "$new_active_target" ]]; then
                    rm -f -- "$active_path" || failed=1
                else
                    failed=1
                fi
            fi
            if ((failed == 0 && active_detached)); then
                if [[ -e $active_path || -L $active_path || ! -L $transaction_dir/active.previous ]]; then
                    failed=1
                else
                    mv "$transaction_dir/active.previous" "$active_path" || failed=1
                fi
            fi
        fi
    fi

    if ((failed == 0 && version_installed)); then
        if rollback_failpoint_matches before_remove_new_version; then
            echo "installer rollback failpoint triggered: before_remove_new_version" >&2
            failed=1
        elif [[ -n $version_dir && -d $version_dir && ! -L $version_dir && $version_dir == "$install_root/versions/"* ]]; then
            rm -rf -- "$version_dir" || failed=1
            if ((failed == 0)); then version_installed=0; fi
        else
            failed=1
        fi
    fi

    if ((failed == 0)); then
        if [[ -n $backup_dir && -d $backup_dir && ! -L $backup_dir ]]; then
            rm -rf -- "$backup_dir" || failed=1
        fi
        if ((failed == 0)) && [[ -n $transaction_dir && -d $transaction_dir && ! -L $transaction_dir ]]; then
            rm -rf -- "$transaction_dir" || failed=1
        fi
    fi
    if ((failed)); then
        rollback_failed=1
        if [[ -n $transaction_dir ]]; then
            echo "rollback was incomplete; recovery materials remain under the install root" >&2
        else
            echo "rollback was incomplete" >&2
        fi
    fi
}

finish() {
    local status=$?
    trap - EXIT
    if ((status != 0 && committed == 0)); then
        if journal_records_commit; then
            committed=1
            echo "installation commit was persisted; recovery cleanup remains pending" >&2
        else
            rollback
        fi
    fi
    if ((rollback_failed == 0)) && [[ -n $stage && -d $stage ]]; then rm -rf -- "$stage"; fi
    if [[ -n $active_next && -L $active_next ]]; then rm -f -- "$active_next"; fi
    if [[ -n $entry_next && -L $entry_next ]]; then rm -f -- "$entry_next"; fi
    if [[ -n $launcher_tmp && -f $launcher_tmp ]]; then rm -f -- "$launcher_tmp"; fi
    if ((bundle_fd_open)); then exec 9<&-; fi
    if [[ -n $download_dir && -d $download_dir && ! -L $download_dir ]]; then rm -rf -- "$download_dir"; fi
    if ((test_mode)) && [[ -n ${temp_parent:-} && -d $temp_parent && ! -L $temp_parent ]]; then rmdir "$temp_parent" 2>/dev/null || true; fi
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if ((offline)); then
    cat <&9 >"$archive_file"
    exec 9<&-
    bundle_fd_open=0
    chmod 0600 "$archive_file"
    if [[ $(size_file "$archive_file") -ne $bundle_size ]]; then
        echo "offline bundle copy did not preserve the opened source" >&2
        exit 1
    fi
    expected_archive_hash=$(lower_hex "$bundle_sha256")
else
    curl --fail --location --silent --show-error \
        --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --connect-timeout 15 --max-time 300 --max-filesize 1048576 \
        --output "$checksums_file" "$release_base/SHA256SUMS"
    curl --fail --location --silent --show-error \
        --proto '=https' --proto-redir '=https' --tlsv1.2 \
        --connect-timeout 15 --max-time 300 --max-filesize 104857600 \
        --output "$archive_file" "$release_base/$archive_name"
    chmod 0600 "$checksums_file" "$archive_file"

    if [[ ! -f $checksums_file || -L $checksums_file || $(size_file "$checksums_file") -gt 1048576 ]]; then
        echo "release checksum file exceeds the 1 MiB limit or is unsafe" >&2
        exit 1
    fi
    outer_allowed='|install.sh|install.ps1|ipgw-meta-darwin-amd64.tar.gz|ipgw-meta-darwin-arm64.tar.gz|ipgw-meta-linux-amd64.tar.gz|ipgw-meta-linux-arm64.tar.gz|ipgw-meta-windows-amd64.zip|ipgw-meta-windows-arm64.zip|'
    outer_seen='|'
    outer_count=0
    expected_archive_hash=''
    while read -r expected file extra; do
        if [[ ! $expected =~ ^[0-9A-Fa-f]{64}$ || -n ${extra:-} ]]; then
            echo "invalid release checksum record" >&2
            exit 1
        fi
        case "$outer_allowed" in *"|$file|"*) ;; *) echo "unexpected release checksum target" >&2; exit 1 ;; esac
        case "$outer_seen" in *"|$file|"*) echo "duplicate release checksum target" >&2; exit 1 ;; esac
        outer_seen="$outer_seen$file|"
        ((outer_count += 1))
        if [[ $file == "$archive_name" ]]; then
            expected_archive_hash=$(lower_hex "$expected")
        fi
    done <"$checksums_file"
    if [[ $outer_count -ne 8 || -z $expected_archive_hash ]]; then
        echo "release checksum manifest is incomplete" >&2
        exit 1
    fi
fi
if [[ ! -f $archive_file || -L $archive_file ]]; then
    echo "acquired archive is not a private regular file" >&2
    exit 1
fi
archive_size=$(size_file "$archive_file")
if ((archive_size < 1 || archive_size > 104857600)); then
    echo "release archive must be between 1 byte and 100 MiB" >&2
    exit 1
fi
actual_hash=$(hash_file "$archive_file")
if [[ $actual_hash != "$expected_archive_hash" ]]; then
    echo "acquired archive failed SHA-256 verification" >&2
    exit 1
fi

expected_entries=$(printf '%s\n' ipgw ipgw-meta ipgw-legacy LICENSE launcher-default.yaml bundle-manifest.json SHA256SUMS | LC_ALL=C sort)
archive_entries=$(tar -tzf "$archive_file" | LC_ALL=C sort)
if [[ $archive_entries != "$expected_entries" ]]; then
    echo "release archive contains an unexpected file set" >&2
    exit 1
fi
archive_type_count=0
declared_total=0
while IFS= read -r archive_line; do
    if [[ ${archive_line:0:1} != '-' ]]; then
        echo "release archive contains a link or non-regular member" >&2
        exit 1
    fi
    read -r -a archive_fields <<<"$archive_line"
    if ((${#archive_fields[@]} < 6)); then
        echo "release archive has an unreadable type or size record" >&2
        exit 1
    fi
    archive_member=${archive_fields[${#archive_fields[@]}-1]}
    if [[ $kernel == Darwin ]]; then
        archive_member_size=${archive_fields[4]:-}
    else
        archive_member_size=${archive_fields[2]:-}
    fi
    if [[ ! $archive_member_size =~ ^[0-9]+$ ]]; then
        echo "release archive has an invalid declared member size" >&2
        exit 1
    fi
    case "$archive_member" in
        ipgw|ipgw-meta|ipgw-legacy) archive_member_limit=67108864 ;;
        LICENSE) archive_member_limit=4194304 ;;
        launcher-default.yaml|bundle-manifest.json|SHA256SUMS) archive_member_limit=65536 ;;
        *) echo "release archive type listing contains an unexpected member" >&2; exit 1 ;;
    esac
    if ((archive_member_size < 1 || archive_member_size > archive_member_limit)); then
        echo "release archive member exceeds its declared size limit: $archive_member" >&2
        exit 1
    fi
    ((declared_total += archive_member_size))
    if ((declared_total > 205717504)); then
        echo "release archive exceeds its total decompressed size limit" >&2
        exit 1
    fi
    ((archive_type_count += 1))
done < <(tar -tvzf "$archive_file")
if [[ $archive_type_count -ne 7 ]]; then
    echo "release archive type listing is incomplete" >&2
    exit 1
fi
if ((declared_total > archive_size * 200)); then
    echo "release archive exceeds the maximum compression ratio" >&2
    exit 1
fi

had_install=0
if [[ -e $bin_dir/ipgw || -L $bin_dir/ipgw ]] || command -v ipgw >/dev/null 2>&1; then
    had_install=1
fi
if [[ -e $config_base/ipgw/config.yaml || -e $HOME/.ipgw ]]; then
    had_install=1
fi

ensure_real_directory "$install_root" 0755
install_root=$(cd "$install_root" && pwd -P)
if [[ $install_root == / ]]; then
    echo "install root cannot be the filesystem root" >&2
    exit 1
fi
validate_directory_ancestors "$install_root" "install root"
ensure_real_directory "$install_root/versions" 0755
ensure_real_directory "$bin_dir" 0755
bin_dir=$(cd "$bin_dir" && pwd -P)
if [[ $bin_dir == / ]]; then
    echo "binary directory cannot be the filesystem root" >&2
    exit 1
fi
validate_directory_ancestors "$bin_dir" "binary directory"
if paths_overlap "$bin_dir" "$install_root" || paths_overlap "$bin_dir" "$install_root/versions"; then
    echo "binary directory must be distinct from the install and versions directories" >&2
    exit 1
fi
for pending_path in \
    "$install_root"/.transaction.* \
    "$install_root"/.staging.* \
    "$install_root"/.active-next.* \
    "$bin_dir"/.ipgw-meta-backup.* \
    "$bin_dir"/.ipgw.next.* \
    "$bin_dir"/.ipgw-meta.next.* \
    "$bin_dir"/.ipgw-legacy.next.*; do
    if [[ -e $pending_path || -L $pending_path ]]; then
        echo "an unfinished installer transaction requires recovery before continuing" >&2
        exit 1
    fi
done
stage=$(mktemp -d "$install_root/.staging.XXXXXX")
chmod 0700 "$stage"

extract_member() {
    local name=$1
    local max_bytes=$2
    local max_blocks=$(( (max_bytes + 1023) / 1024 ))
    (
        ulimit -f "$max_blocks"
        tar -xzf "$archive_file" -C "$stage" "$name"
    )
    if [[ ! -f $stage/$name || -L $stage/$name ]]; then
        echo "bundle member is not a regular file after extraction: $name" >&2
        exit 1
    fi
    if [[ $(size_file "$stage/$name") -gt $max_bytes ]]; then
        echo "bundle member exceeds its decompressed size limit: $name" >&2
        exit 1
    fi
}

extract_member ipgw 67108864
extract_member ipgw-meta 67108864
extract_member ipgw-legacy 67108864
extract_member LICENSE 4194304
extract_member launcher-default.yaml 65536
extract_member bundle-manifest.json 65536
extract_member SHA256SUMS 65536

actual_total=0
for extracted_name in ipgw ipgw-meta ipgw-legacy LICENSE launcher-default.yaml bundle-manifest.json SHA256SUMS; do
    ((actual_total += $(size_file "$stage/$extracted_name")))
done
if ((actual_total != declared_total || actual_total > 205717504 || actual_total > archive_size * 200)); then
    echo "release archive decompressed size did not match its bounded declaration" >&2
    exit 1
fi

allowed='|ipgw|ipgw-meta|ipgw-legacy|LICENSE|launcher-default.yaml|bundle-manifest.json|'
seen='|'
verified_count=0
while read -r expected file extra; do
    if [[ ! $expected =~ ^[0-9A-Fa-f]{64}$ || -n ${extra:-} ]]; then
        echo "invalid internal checksum record" >&2
        exit 1
    fi
    case "$allowed" in *"|$file|"*) ;; *) echo "unexpected internal checksum target" >&2; exit 1 ;; esac
    case "$seen" in *"|$file|"*) echo "duplicate internal checksum target" >&2; exit 1 ;; esac
    if [[ ! -f $stage/$file || -L $stage/$file ]]; then
        echo "bundle member is missing or not a regular file: $file" >&2
        exit 1
    fi
    actual=$(hash_file "$stage/$file")
    expected=$(lower_hex "$expected")
    if [[ $actual != "$expected" ]]; then
        echo "bundle member failed SHA-256 verification: $file" >&2
        exit 1
    fi
    seen="$seen$file|"
    ((verified_count += 1))
done <"$stage/SHA256SUMS"
if [[ $verified_count -ne 6 ]]; then
    echo "bundle checksum manifest is incomplete" >&2
    exit 1
fi

manifest_file="$stage/bundle-manifest.json"
manifest_line_count=$(wc -l <"$manifest_file" | tr -d '[:space:]')
if [[ $manifest_line_count -ne 16 ]]; then
    echo "bundle manifest is not in the canonical v1 format" >&2
    exit 1
fi
manifest_version_line=$(sed -n '5p' "$manifest_file")
manifest_prefix='  "version": "'
manifest_suffix='",'
case "$manifest_version_line" in
    "$manifest_prefix"*"$manifest_suffix") ;;
    *) echo "bundle manifest has an invalid version record" >&2; exit 1 ;;
esac
manifest_version=${manifest_version_line#"$manifest_prefix"}
manifest_version=${manifest_version%"$manifest_suffix"}
case "$manifest_version" in
    ''|*[!0-9A-Za-z._+-]*) echo "bundle manifest has an invalid version" >&2; exit 1 ;;
esac
if [[ -n $expected_version && $manifest_version != "$expected_version" ]]; then
    echo "bundle manifest version does not match the pinned installer" >&2
    exit 1
fi

entry0_hash=$(hash_file "$stage/ipgw")
entry1_hash=$(hash_file "$stage/ipgw-meta")
entry2_hash=$(hash_file "$stage/ipgw-legacy")
entry0_size=$(size_file "$stage/ipgw")
entry1_size=$(size_file "$stage/ipgw-meta")
entry2_size=$(size_file "$stage/ipgw-legacy")
expected_manifest="$download_dir/expected-bundle-manifest.json"
cat >"$expected_manifest" <<EOF
{
  "schema_version": 1,
  "product": "ipgw-meta",
  "module": "github.com/UnbalancedCat/ipgw-meta",
  "version": "$manifest_version",
  "platform": "$target",
  "entries": [
    {"path": "ipgw", "sha256": "$entry0_hash", "size": $entry0_size},
    {"path": "ipgw-meta", "sha256": "$entry1_hash", "size": $entry1_size},
    {"path": "ipgw-legacy", "sha256": "$entry2_hash", "size": $entry2_size}
  ],
  "launcher_default": "meta",
  "layout": "versioned-bundle-v1",
  "self_update": false,
  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}
}
EOF
if ! cmp -s "$expected_manifest" "$manifest_file"; then
    echo "bundle manifest does not exactly bind the three entry paths, hashes, and sizes" >&2
    exit 1
fi

expected_launcher="$download_dir/expected-launcher-default.yaml"
printf 'schema_version: 1\nmode: meta\ncohort: new-install\n' >"$expected_launcher"
if ! cmp -s "$expected_launcher" "$stage/launcher-default.yaml"; then
    echo "bundle launcher metadata is invalid" >&2
    exit 1
fi
chmod 0755 "$stage/ipgw" "$stage/ipgw-meta" "$stage/ipgw-legacy"
chmod 0644 "$stage/LICENSE" "$stage/launcher-default.yaml" "$stage/bundle-manifest.json" "$stage/SHA256SUMS"

maybe_fail after_verified_stage

ensure_real_directory "$launcher_dir" 0755
validate_directory_ancestors "$launcher_dir" "launcher directory"
for pending_path in "$launcher_dir"/.launcher.*; do
    if [[ -e $pending_path || -L $pending_path ]]; then
        echo "an unfinished launcher transaction requires recovery before continuing" >&2
        exit 1
    fi
done
if [[ -L $launcher_file || (-e $launcher_file && ! -f $launcher_file) ]]; then
    echo "refusing a non-regular or symbolic-link launcher configuration: $launcher_file" >&2
    exit 1
fi
if [[ ! -e $launcher_file && ! -L $launcher_file ]]; then
    mode=meta
    cohort=new-install
    if ((had_install)); then
        mode=legacy
        cohort=existing-install
    fi
    launcher_tmp=$(mktemp "$launcher_dir/.launcher.XXXXXX")
    printf 'schema_version: 1\nmode: %s\ncohort: %s\nchosen_at: %s\n' \
        "$mode" "$cohort" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >"$launcher_tmp"
    chmod 0600 "$launcher_tmp"
fi

active_path="$install_root/active"
if [[ -L $active_path ]]; then
    had_active=1
    old_active=$(readlink "$active_path")
    case "$old_active" in
        versions/*)
            old_active_name=${old_active#versions/}
            if [[ ! $old_active_name =~ ^[0-9a-f]{16}-[0-9]{14}-[0-9]+$ ]]; then
                echo "refusing unmanaged active link target" >&2
                exit 1
            fi
            old_active_path="$install_root/versions/$old_active_name"
            if [[ -L $old_active_path || ! -d $old_active_path ]]; then
                echo "refusing an active link whose version target is not a real directory" >&2
                exit 1
            fi
            ;;
        *) echo "refusing unmanaged active link target" >&2; exit 1 ;;
    esac
elif [[ -e $active_path ]]; then
    echo "refusing to replace unmanaged active path: $active_path" >&2
    exit 1
fi

for name in ipgw ipgw-meta ipgw-legacy; do
    destination="$bin_dir/$name"
    if [[ -d $destination && ! -L $destination ]]; then
        echo "refusing to replace directory: $destination" >&2
        exit 1
    fi
    if [[ -e $destination && ! -f $destination && ! -L $destination ]]; then
        echo "refusing to replace a non-regular binary entry: $destination" >&2
        exit 1
    fi
    if [[ -L $destination && $(readlink "$destination") != "$active_path/$name" ]]; then
        echo "refusing to replace an unmanaged binary link: $destination" >&2
        exit 1
    fi
done

hash_prefix=${actual_hash:0:16}
version_dir="$install_root/versions/$hash_prefix-$(date -u +%Y%m%d%H%M%S)-$$"
if [[ -e $version_dir || -L $version_dir ]]; then
    echo "refusing to overwrite an existing version directory" >&2
    exit 1
fi
transaction_dir=$(mktemp -d "$install_root/.transaction.XXXXXX")
chmod 0700 "$transaction_dir"
journal_file="$transaction_dir/journal"
write_journal verified-stage

version_installed=1
mv "$stage" "$version_dir"
stage=""
chmod 0755 "$version_dir"
write_journal version-published
maybe_fail after_version_publish

new_active_target="versions/${version_dir##*/}"
active_next="$install_root/.active-next.$$"
ln -s "$new_active_target" "$active_next"
if ((had_active)); then
    active_detached=1
    mv "$active_path" "$transaction_dir/active.previous"
fi
write_journal old-active-detached
maybe_fail after_old_active_detach

if [[ -e $active_path || -L $active_path ]]; then
    echo "active path appeared during installation" >&2
    exit 1
fi
active_switched=1
mv "$active_next" "$active_path"
active_next=""
write_journal active-switched
maybe_fail after_active_switch

backup_dir=$(mktemp -d "$bin_dir/.ipgw-meta-backup.XXXXXX")
chmod 0700 "$backup_dir"
entry_index=0
for name in ipgw ipgw-meta ipgw-legacy; do
    destination="$bin_dir/$name"
    if [[ -e $destination || -L $destination ]]; then
        if [[ -L $destination && $(readlink "$destination") != "$active_path/$name" ]]; then
            echo "binary entry changed to an unmanaged link during installation" >&2
            exit 1
        fi
        if [[ ! -L $destination && ! -f $destination ]]; then
            echo "binary entry changed to a non-regular path during installation" >&2
            exit 1
        fi
        backed_entries+=("$name")
        mv "$destination" "$backup_dir/$name"
    fi
    entry_next="$bin_dir/.${name}.next.$$"
    ln -s "$active_path/$name" "$entry_next"
    installed_entries+=("$name")
    mv "$entry_next" "$destination"
    entry_next=""
    ((entry_index += 1))
    write_journal "entry-$entry_index-published"
    if ((entry_index == 1)); then maybe_fail after_entry_1; fi
    if ((entry_index == 2)); then maybe_fail after_entry_2; fi
done

if [[ -L $launcher_file || (-e $launcher_file && ! -f $launcher_file) ]]; then
    echo "launcher configuration changed to a non-regular path during installation" >&2
    exit 1
fi
if [[ -n $launcher_tmp ]]; then
    if [[ -e $launcher_file ]]; then
        rm -f -- "$launcher_tmp"
    else
        launcher_installed=1
        mv "$launcher_tmp" "$launcher_file"
    fi
    launcher_tmp=""
fi
write_journal launcher-published
maybe_fail after_launcher_publish

write_journal path-handled
maybe_fail after_path_update
write_journal ready-to-commit
maybe_fail before_commit
write_journal committed
committed=1
version_installed=0

if [[ -n $backup_dir && -d $backup_dir && ! -L $backup_dir ]]; then
    rm -rf -- "$backup_dir"
    backup_dir=""
fi
if [[ -n $transaction_dir && -d $transaction_dir && ! -L $transaction_dir ]]; then
    rm -rf -- "$transaction_dir"
    transaction_dir=""
fi

echo "IPGW-Meta installed as one three-entry bundle in $version_dir"
echo "Launcher mode was preserved for existing installs; new installs default to meta."
case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "Add $bin_dir to PATH before running ipgw." >&2 ;;
esac
