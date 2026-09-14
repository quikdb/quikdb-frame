#!/bin/sh
# Install the official release binary without a Go toolchain or root privileges.
set -eu
version=""
verify_provenance=false
while [ "$#" -gt 0 ]; do
  case "$1" in
    --version) [ "$#" -ge 2 ] || { echo 'Missing --version value' >&2; exit 1; }; version=$2; shift 2 ;;
    --verify-provenance) verify_provenance=true; shift ;;
    *) echo "Unknown option: $1" >&2; exit 1 ;;
  esac
done
for tool in curl awk mktemp; do command -v "$tool" >/dev/null || { echo "Required tool missing: $tool" >&2; exit 1; }; done
case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) asset=quikdb-frame-linux-amd64 ;;
  Linux/aarch64|Linux/arm64) asset=quikdb-frame-linux-arm64 ;;
  Darwin/x86_64) asset=quikdb-frame-darwin-amd64 ;;
  Darwin/arm64) asset=quikdb-frame-darwin-arm64 ;;
  *) echo 'Unsupported platform; use a supported release binary.' >&2; exit 1 ;;
esac
if [ -z "$version" ]; then
  release_url=$(curl --fail --silent --show-error --location --max-time 30 --output /dev/null --write-out '%{url_effective}' https://github.com/quikdb/quikdb-frame/releases/latest)
  case "$release_url" in https://github.com/quikdb/quikdb-frame/releases/tag/*) version=${release_url##*/} ;; *) echo 'Invalid official release redirect' >&2; exit 1 ;; esac
fi
printf '%s\n' "$version" | awk 'NR == 1 && /^v[0-9]+\.[0-9]+\.[0-9]+$/ {valid=1} END {exit !(NR == 1 && valid)}' || { echo 'A stable version such as v0.1.11 is required' >&2; exit 1; }
if command -v sha256sum >/dev/null; then checksum_tool=sha256sum
elif command -v shasum >/dev/null; then checksum_tool=shasum
else echo 'Required SHA-256 utility missing (sha256sum or shasum)' >&2; exit 1
fi
if "$verify_provenance"; then command -v gh >/dev/null || { echo '--verify-provenance requires GitHub CLI (gh)' >&2; exit 1; }; fi
install_dir=${QUIKDB_FRAME_INSTALL_DIR:-${HOME:?User home directory is required}/.local/bin}
work_dir=$(mktemp -d)
candidate=""
trap 'rm -rf "$work_dir"; if [ -n "$candidate" ]; then rm -f "$candidate"; fi' EXIT
trap 'exit 130' INT
trap 'exit 129' HUP
trap 'exit 143' TERM
base=https://github.com/quikdb/quikdb-frame/releases/download/$version
curl --fail --silent --show-error --location --max-time 90 --max-filesize 67108864 "$base/$asset" --output "$work_dir/$asset"
curl --fail --silent --show-error --location --max-time 30 "$base/SHA256SUMS" --output "$work_dir/SHA256SUMS"
awk -v file="$asset" '$2 == file {print; found++} END {if (found != 1) exit 1}' "$work_dir/SHA256SUMS" > "$work_dir/selected-sum" || { echo 'Release checksum is missing or ambiguous' >&2; exit 1; }
if [ "$checksum_tool" = sha256sum ]; then (cd "$work_dir" && sha256sum -c selected-sum)
else (cd "$work_dir" && shasum -a 256 -c selected-sum)
fi
if "$verify_provenance"; then
  gh attestation verify "$work_dir/$asset" --repo quikdb/quikdb-frame --signer-workflow quikdb/quikdb-frame/.github/workflows/release.yml --source-ref "refs/tags/$version"
fi
mkdir -p "$install_dir"
if [ -d "$install_dir/quikdb-frame" ]; then echo "Installation target is a directory" >&2; exit 1; fi
candidate=$(mktemp "$install_dir/.quikdb-frame.XXXXXX")
cp "$work_dir/$asset" "$candidate"
chmod 755 "$candidate"
mv -f "$candidate" "$install_dir/quikdb-frame"
candidate=""
printf 'Installed %s at %s/quikdb-frame (SHA-256 verified).\n' "$version" "$install_dir"
if "$verify_provenance"; then printf 'Signed build provenance verified.\n'; fi
printf 'Add %s to PATH, then run quikdb-frame login.\n' "$install_dir"
