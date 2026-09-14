#!/bin/sh
# Isolated adversarial download fixtures: no real downloads, user PATH or credentials.
set -eu
installer=$(pwd)/scripts/install.sh
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT HUP INT TERM
mkdir -p "$root/tools" "$root/install"
printf '#!/bin/sh\nprintf "fixture binary\\n"\n' > "$root/binary"
sha256sum "$root/binary" | awk '{print $1 "  quikdb-frame-linux-amd64"}' > "$root/SHA256SUMS"
cat > "$root/tools/curl" <<'MOCK'
#!/bin/sh
output=""; url=""
while [ "$#" -gt 0 ]; do
 case "$1" in
 --output) output=$2; shift 2 ;;
 --max-time|--write-out) shift 2 ;;
 --*) shift ;;
 *) url=$1; shift ;;
 esac
done
case "$url" in
 https://github.com/quikdb/quikdb-frame/releases/latest) printf 'https://github.com/quikdb/quikdb-frame/releases/tag/v0.1.11' ;;
 */download/v0.1.11/SHA256SUMS) cp "$INSTALL_FIXTURE_ROOT/SHA256SUMS" "$output" ;;
 */download/v0.1.11/quikdb-frame-linux-amd64) cp "$INSTALL_FIXTURE_ROOT/binary" "$output"; if [ "${INSTALL_TAMPER:-false}" = true ]; then printf 'tampered' >> "$output"; fi ;;
 *) echo 'Unexpected or unpinned fixture URL' >&2; exit 1 ;;
 esac
MOCK
cat > "$root/tools/uname" <<'MOCK'
#!/bin/sh
case "$1" in -s) printf 'Linux\n';; -m) printf 'x86_64\n';; *) exit 1;; esac
MOCK
cat > "$root/tools/gh" <<'MOCK'
#!/bin/sh
# A failed signature must leave the installed binary untouched.
exit 1
MOCK
chmod +x "$root/tools/"*
export INSTALL_FIXTURE_ROOT="$root" QUIKDB_FRAME_INSTALL_DIR="$root/install"
export PATH="$root/tools:$PATH"
sh "$installer" > "$root/success.log"
cmp "$root/binary" "$root/install/quikdb-frame"
printf 'existing binary' > "$root/install/quikdb-frame"
if INSTALL_TAMPER=true sh "$installer" --version v0.1.11 > "$root/tamper.log" 2>&1; then echo 'Tampered binary was installed' >&2; exit 1; fi
test "$(cat "$root/install/quikdb-frame")" = 'existing binary'
if sh "$installer" --version v0.1.11 --verify-provenance > "$root/signature.log" 2>&1; then echo 'Failed signature was ignored' >&2; exit 1; fi
test "$(cat "$root/install/quikdb-frame")" = 'existing binary'
if sh "$installer" --version '../../untrusted' > "$root/version.log" 2>&1; then echo 'Invalid version accepted' >&2; exit 1; fi
if find "$root/install" -name '.quikdb-frame.*' | awk 'END {exit NR != 0}'; then :; else echo 'Candidate leaked' >&2; exit 1; fi
printf 'Installer pinned download/checksum/tamper/signature rejection and existing-binary preservation passed.\n'
