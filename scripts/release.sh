#!/usr/bin/env bash
set -euo pipefail
umask 077
export LC_ALL=C

if [[ $# -ne 3 ]]; then
    echo "usage: $0 BUILD_DIR RELEASE_DIR RELEASE_VERSION_FILE" >&2
    exit 2
fi

build_dir=$(cd "$1" && pwd)
release_dir=$2
version_file=$3

if [[ ! -f $version_file || -L $version_file ]]; then
    echo "RELEASE_VERSION_FILE must name a readable regular file" >&2
    exit 2
fi
version_bytes=$(wc -c <"$version_file" | tr -d '[:space:]')
if [[ $version_bytes -gt 256 ]]; then
    echo "release version file exceeds 256 bytes" >&2
    exit 2
fi
version=$(cat -- "$version_file")
if [[ -z $version || ! $version =~ ^[0-9A-Za-z._+-]+$ ]]; then
    echo "release version contains unsupported characters" >&2
    exit 2
fi

mkdir -p "$release_dir"
release_dir=$(cd "$release_dir" && pwd)
license_file=$(cd "$(dirname "$0")/.." && pwd)/LICENSE
repository_root=$(cd "$(dirname "$0")/.." && pwd)
install_sh_source="$repository_root/install.sh"
install_ps1_source="$repository_root/install.ps1"

if [[ ! -f $license_file ]]; then
    echo "LICENSE is required for release bundles" >&2
    exit 1
fi
if [[ ! -f $install_sh_source || ! -f $install_ps1_source ]]; then
    echo "install.sh and install.ps1 are required release assets" >&2
    exit 1
fi

hash_file() {
    sha256sum "$1" | awk '{print tolower($1)}'
}

size_file() {
    wc -c <"$1" | tr -d '[:space:]'
}

publish_file() {
    local source=$1
    local destination=$2
    if [[ (-e $destination || -L $destination) && (! -f $destination || -L $destination) ]]; then
        echo "refusing to replace a non-regular or symbolic-link release asset: $destination" >&2
        exit 1
    fi
    mv -f -- "$source" "$destination"
}

targets=(
    darwin-amd64
    darwin-arm64
    linux-amd64
    linux-arm64
    windows-amd64
    windows-arm64
)

staging_dirs=()
temporary_files=()
cleanup() {
    local path
    for path in "${staging_dirs[@]:-}"; do
        if [[ -n $path && -d $path ]]; then
            rm -rf -- "$path"
        fi
    done
    for path in "${temporary_files[@]:-}"; do
        if [[ -n $path && -f $path ]]; then
            rm -f -- "$path"
        fi
    done
}
trap cleanup EXIT

archives=()
for target in "${targets[@]}"; do
    source_dir="$build_dir/$target"
    if [[ ! -d $source_dir ]]; then
        echo "missing build directory: $source_dir" >&2
        exit 1
    fi

    stage=$(mktemp -d "$release_dir/.${target}.XXXXXX")
    staging_dirs+=("$stage")
    suffix=""
    archive="ipgw-meta-${target}.tar.gz"
    if [[ $target == windows-* ]]; then
        suffix=.exe
        archive="ipgw-meta-${target}.zip"
    fi
    archives+=("$archive")

    entries=()
    for binary in ipgw ipgw-meta ipgw-legacy; do
        source_file="$source_dir/$binary$suffix"
        if [[ ! -f $source_file || -L $source_file || ! -s $source_file ]]; then
            echo "missing release binary: $source_file" >&2
            exit 1
        fi
        source_size=$(size_file "$source_file")
        if [[ $source_size -gt 67108864 ]]; then
            echo "release binary exceeds the 64 MiB per-entry limit: $source_file" >&2
            exit 1
        fi
        cp "$source_file" "$stage/$binary$suffix"
        entries+=("$binary$suffix")
    done
    cp "$license_file" "$stage/LICENSE"

    cat >"$stage/launcher-default.yaml" <<EOF
schema_version: 1
mode: meta
cohort: new-install
EOF

    entry0_hash=$(hash_file "$stage/${entries[0]}")
    entry1_hash=$(hash_file "$stage/${entries[1]}")
    entry2_hash=$(hash_file "$stage/${entries[2]}")
    entry0_size=$(size_file "$stage/${entries[0]}")
    entry1_size=$(size_file "$stage/${entries[1]}")
    entry2_size=$(size_file "$stage/${entries[2]}")

    cat >"$stage/bundle-manifest.json" <<EOF
{
  "schema_version": 1,
  "product": "ipgw-meta",
  "module": "github.com/UnbalancedCat/ipgw-meta",
  "version": "$version",
  "platform": "$target",
  "entries": [
    {"path": "${entries[0]}", "sha256": "$entry0_hash", "size": $entry0_size},
    {"path": "${entries[1]}", "sha256": "$entry1_hash", "size": $entry1_size},
    {"path": "${entries[2]}", "sha256": "$entry2_hash", "size": $entry2_size}
  ],
  "launcher_default": "meta",
  "layout": "versioned-bundle-v1",
  "self_update": false,
  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}
}
EOF

    (
        cd "$stage"
        sha256sum "${entries[@]}" LICENSE launcher-default.yaml bundle-manifest.json >SHA256SUMS
    )

    archive_tmp="$stage/.bundle-output"
    if [[ $target == windows-* ]]; then
        archive_tmp="$archive_tmp.zip"
        (
            cd "$stage"
            zip -q -X "$archive_tmp" "${entries[@]}" LICENSE launcher-default.yaml bundle-manifest.json SHA256SUMS
        )
    else
        archive_tmp="$archive_tmp.tar.gz"
        tar --sort=name --owner=0 --group=0 --numeric-owner \
            -czf "$archive_tmp" -C "$stage" \
            "${entries[@]}" LICENSE launcher-default.yaml bundle-manifest.json SHA256SUMS
    fi
    archive_size=$(size_file "$archive_tmp")
    if [[ $archive_size -gt 104857600 ]]; then
        echo "release archive exceeds the 100 MiB download limit: $archive" >&2
        exit 1
    fi
    publish_file "$archive_tmp" "$release_dir/$archive"
done

install_sh_tmp=$(mktemp "$release_dir/.install.sh.XXXXXX")
temporary_files+=("$install_sh_tmp")
{
    printf '#!/usr/bin/env bash\n'
    printf "IPGW_VERSION='%s'\nexport IPGW_VERSION\n" "$version"
    tail -n +2 "$install_sh_source"
} >"$install_sh_tmp"
chmod 0755 "$install_sh_tmp"
publish_file "$install_sh_tmp" "$release_dir/install.sh"

install_ps1_tmp=$(mktemp "$release_dir/.install.ps1.XXXXXX")
temporary_files+=("$install_ps1_tmp")
awk -v version="$version" '
    { print }
    !inserted && $0 == ")" {
        print ""
        print "# Generated release asset: remain pinned to this release batch."
        print "$Version = \047" version "\047"
        inserted = 1
    }
    END { if (!inserted) exit 42 }
' "$install_ps1_source" >"$install_ps1_tmp"
publish_file "$install_ps1_tmp" "$release_dir/install.ps1"

checksums_tmp=$(mktemp "$release_dir/.SHA256SUMS.XXXXXX")
temporary_files+=("$checksums_tmp")
(
    cd "$release_dir"
    sha256sum install.sh install.ps1 "${archives[@]}" >"$checksums_tmp"
)
publish_file "$checksums_tmp" "$release_dir/SHA256SUMS"

echo "created six atomic bundles and two version-pinned installers in $release_dir"
