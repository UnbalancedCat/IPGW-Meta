#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C

repo_url="https://github.com/UnbalancedCat/ipgw-meta"
install_root=${IPGW_INSTALL_ROOT:-/usr/local/lib/ipgw-meta}
bin_dir=${IPGW_BIN_DIR:-/usr/local/bin}

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
if [[ -n ${IPGW_VERSION:-} ]]; then
    if [[ ! $IPGW_VERSION =~ ^v?[0-9A-Za-z][0-9A-Za-z._+-]*$ ]]; then
        echo "invalid IPGW_VERSION" >&2
        exit 2
    fi
    release_base="$repo_url/releases/download/$IPGW_VERSION"
else
    release_base="$repo_url/releases/latest/download"
fi

if ! command -v curl >/dev/null 2>&1; then
    echo "curl is required to install IPGW-Meta" >&2
    exit 2
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

ensure_real_directory() {
    local path=$1
    if [[ -L $path ]]; then
        echo "refusing to use a symbolic-link directory: $path" >&2
        exit 1
    fi
    if [[ -e $path && ! -d $path ]]; then
        echo "refusing to use a non-directory path: $path" >&2
        exit 1
    fi
    mkdir -p "$path"
    if [[ -L $path || ! -d $path ]]; then
        echo "failed to create a real directory: $path" >&2
        exit 1
    fi
}

download_dir=$(mktemp -d "${TMPDIR:-/tmp}/ipgw-meta-download.XXXXXX")
archive_file="$download_dir/$archive_name"
checksums_file="$download_dir/SHA256SUMS"
stage=""
active_next=""
backup_dir=""
version_dir=""
version_installed=0
launcher_tmp=""
launcher_installed=0
active_switched=0
committed=0
had_active=0
old_active=""
installed_entries=""
backed_entries=""

atomic_replace_link() {
    local source=$1
    local destination=$2
    if [[ -e $destination || -L $destination ]]; then
        case "$kernel" in
            Linux) mv -Tf "$source" "$destination" ;;
            Darwin) mv -fh "$source" "$destination" ;;
        esac
    else
        mv "$source" "$destination"
    fi
}

rollback() {
    local name
    local failed=0
    set +e
    for name in $installed_entries; do
        rm -f -- "$bin_dir/$name" || failed=1
    done
    for name in $backed_entries; do
        mv "$backup_dir/$name" "$bin_dir/$name" || failed=1
    done
    if ((active_switched)); then
        if ((had_active)); then
            local restore_link="$install_root/.active-restore.$$"
            rm -f -- "$restore_link"
            ln -s "$old_active" "$restore_link" && atomic_replace_link "$restore_link" "$install_root/active" || failed=1
        else
            rm -f -- "$install_root/active" || failed=1
        fi
    fi
    if ((launcher_installed)); then
        rm -f -- "$launcher_file" || failed=1
    fi
    if ((failed)); then
        echo "rollback was incomplete; preserved transaction data at $backup_dir" >&2
    else
        if [[ -n $backup_dir && -d $backup_dir && ! -L $backup_dir ]]; then
            rm -rf -- "$backup_dir"
            backup_dir=""
        fi
        if ((version_installed)) && [[ -n $version_dir && -d $version_dir && ! -L $version_dir ]]; then
            rm -rf -- "$version_dir"
            version_installed=0
        fi
    fi
}

finish() {
    local status=$?
    trap - EXIT
    if ((status != 0 && committed == 0)); then
        rollback
    fi
    if [[ -n $stage && -d $stage ]]; then rm -rf -- "$stage"; fi
    if [[ -n $active_next && -L $active_next ]]; then rm -f -- "$active_next"; fi
    if [[ -n $launcher_tmp && -f $launcher_tmp ]]; then rm -f -- "$launcher_tmp"; fi
    rm -rf -- "$download_dir"
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

curl --fail --location --silent --show-error \
    --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --connect-timeout 15 --max-time 300 --max-filesize 1048576 \
    --output "$checksums_file" "$release_base/SHA256SUMS"
curl --fail --location --silent --show-error \
    --proto '=https' --proto-redir '=https' --tlsv1.2 \
    --connect-timeout 15 --max-time 300 --max-filesize 104857600 \
    --output "$archive_file" "$release_base/$archive_name"

if [[ $(size_file "$checksums_file") -gt 1048576 ]]; then
    echo "release checksum file exceeds the 1 MiB limit" >&2
    exit 1
fi
if [[ $(size_file "$archive_file") -gt 104857600 ]]; then
    echo "release archive exceeds the 100 MiB limit" >&2
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
actual_hash=$(hash_file "$archive_file")
if [[ $actual_hash != "$expected_archive_hash" ]]; then
    echo "downloaded archive failed SHA-256 verification" >&2
    exit 1
fi

expected_entries=$(printf '%s\n' ipgw ipgw-meta ipgw-legacy LICENSE launcher-default.yaml bundle-manifest.json SHA256SUMS | LC_ALL=C sort)
archive_entries=$(tar -tzf "$archive_file" | LC_ALL=C sort)
if [[ $archive_entries != "$expected_entries" ]]; then
    echo "release archive contains an unexpected file set" >&2
    exit 1
fi
archive_type_count=0
while IFS= read -r archive_line; do
    if [[ ${archive_line:0:1} != '-' ]]; then
        echo "release archive contains a link or non-regular member" >&2
        exit 1
    fi
    ((archive_type_count += 1))
done < <(tar -tvzf "$archive_file")
if [[ $archive_type_count -ne 7 ]]; then
    echo "release archive type listing is incomplete" >&2
    exit 1
fi

had_install=0
if [[ -e $bin_dir/ipgw || -L $bin_dir/ipgw ]] || command -v ipgw >/dev/null 2>&1; then
    had_install=1
fi
if [[ $kernel == Darwin ]]; then
    config_base="$HOME/Library/Application Support"
else
    config_base=${XDG_CONFIG_HOME:-$HOME/.config}
fi
case "$config_base" in
    /*) ;;
    *) echo "user configuration directory must be absolute" >&2; exit 1 ;;
esac
launcher_dir="$config_base/ipgw-meta"
launcher_file="$launcher_dir/launcher.yaml"
if [[ -e $config_base/ipgw/config.yaml || -e $HOME/.ipgw ]]; then
    had_install=1
fi

ensure_real_directory "$install_root"
install_root=$(cd "$install_root" && pwd -P)
if [[ $install_root == / ]]; then
    echo "install root cannot be the filesystem root" >&2
    exit 1
fi
ensure_real_directory "$install_root/versions"
ensure_real_directory "$bin_dir"
bin_dir=$(cd "$bin_dir" && pwd -P)
if [[ $bin_dir == / ]]; then
    echo "binary directory cannot be the filesystem root" >&2
    exit 1
fi
if [[ $bin_dir == "$install_root" || $bin_dir == "$install_root/versions" ]]; then
    echo "binary directory must be distinct from the install and versions directories" >&2
    exit 1
fi
stage=$(mktemp -d "$install_root/.staging.XXXXXX")

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
if [[ -n ${IPGW_VERSION:-} && $manifest_version != "$IPGW_VERSION" ]]; then
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

ensure_real_directory "$launcher_dir"
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

hash_prefix=${actual_hash:0:16}
version_dir="$install_root/versions/$hash_prefix-$(date -u +%Y%m%d%H%M%S)-$$"
mv "$stage" "$version_dir"
stage=""
version_installed=1

active_path="$install_root/active"
if [[ -L $active_path ]]; then
    had_active=1
    old_active=$(readlink "$active_path")
    case "$old_active" in
        versions/*)
            old_active_name=${old_active#versions/}
            case "$old_active_name" in ''|*/*|.|..) echo "refusing unmanaged active link target" >&2; exit 1 ;; esac
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
active_next="$install_root/.active-next.$$"
ln -s "versions/$(basename "$version_dir")" "$active_next"
atomic_replace_link "$active_next" "$active_path"
active_next=""
active_switched=1

backup_dir=$(mktemp -d "$bin_dir/.ipgw-meta-backup.XXXXXX")
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
    if [[ -e $destination || -L $destination ]]; then
        mv "$destination" "$backup_dir/$name"
        backed_entries="$backed_entries $name"
    fi
    entry_next="$bin_dir/.${name}.next.$$"
    ln -s "$active_path/$name" "$entry_next"
    mv "$entry_next" "$destination"
    installed_entries="$installed_entries $name"
done

if [[ -L $launcher_file || (-e $launcher_file && ! -f $launcher_file) ]]; then
    echo "launcher configuration changed to a non-regular path during installation" >&2
    exit 1
fi
if [[ -n $launcher_tmp ]]; then
    if [[ -e $launcher_file ]]; then
        rm -f -- "$launcher_tmp"
    else
        mv "$launcher_tmp" "$launcher_file"
        launcher_installed=1
    fi
    launcher_tmp=""
fi

committed=1
version_installed=0
if [[ -n $backup_dir && -d $backup_dir ]]; then
    rm -rf -- "$backup_dir"
    backup_dir=""
fi

echo "IPGW-Meta installed as one three-entry bundle in $version_dir"
echo "Launcher mode was preserved for existing installs; new installs default to meta."
case ":$PATH:" in
    *":$bin_dir:"*) ;;
    *) echo "Add $bin_dir to PATH before running ipgw." >&2 ;;
esac
