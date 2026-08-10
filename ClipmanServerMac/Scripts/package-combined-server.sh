#!/usr/bin/env zsh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/ClipmanServer/version.txt")"
if [[ ! "$VERSION" =~ '^[0-9]+\.[0-9]+\.[0-9]+$' ]]; then
  echo "Invalid Clipman Server version: $VERSION" >&2
  exit 1
fi
TEMP_ROOT="${CLIPMAN_TEMP_ROOT:-$HOME/Projects/Codex/Temp/clipman}"
DIST="${CLIPMAN_SERVER_COMBINED_OUTPUT_DIR:-$TEMP_ROOT/server-combined-dist}"
mkdir -p "$TEMP_ROOT"
STAGING="$(mktemp -d "$TEMP_ROOT/server-combined.XXXXXX")"
PACKAGE_ROOT="$STAGING/ClipmanServer"
ZIP="$DIST/ClipmanServer-$VERSION.zip"

cleanup() {
  rm -rf "$STAGING"
}
trap cleanup EXIT

mkdir -p "$PACKAGE_ROOT/Windows" "$PACKAGE_ROOT/Linux" "$PACKAGE_ROOT/macOS" "$DIST"
mkdir -p "$PACKAGE_ROOT/Docker"

cp "$ROOT/ClipmanServerLinux/clipman_server.py" "$PACKAGE_ROOT/clipman_server.py"
cp "$ROOT/ClipmanServerLinux/clipman_server_updater.py" "$PACKAGE_ROOT/clipman_server_updater.py"
cp "$ROOT/ClipmanServerLinux/install-clipman-server.sh" "$PACKAGE_ROOT/Linux/install-clipman-server.sh"
cp "$ROOT/ClipmanServerLinux/install-clipman-server-system-helper.sh" "$PACKAGE_ROOT/Linux/install-clipman-server-system-helper.sh"
chmod +x "$PACKAGE_ROOT/Linux/install-clipman-server-system-helper.sh"

for arch in amd64 arm64; do
  (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$VERSION" \
    -o "$PACKAGE_ROOT/Linux/clipman-server-$arch" ./cmd/clipman-server)
  (cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -ldflags="-s -w" \
    -o "$PACKAGE_ROOT/Linux/clipman-server-updater-$arch" ./cmd/clipman-server-updater)
done
(cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$VERSION" \
  -o "$PACKAGE_ROOT/Linux/clipman-server-armv7" ./cmd/clipman-server)
(cd "$ROOT/ClipmanServer" && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" \
  -o "$PACKAGE_ROOT/Linux/clipman-server-updater-armv7" ./cmd/clipman-server-updater)
chmod +x "$PACKAGE_ROOT"/Linux/clipman-server-*
cp "$ROOT/ClipmanServerDocker/Dockerfile.package" "$PACKAGE_ROOT/Docker/Dockerfile"
cp "$ROOT/ClipmanServerDocker/docker-entrypoint.sh" "$PACKAGE_ROOT/Docker/docker-entrypoint.sh"
chmod +x "$PACKAGE_ROOT/Docker/docker-entrypoint.sh"
cp "$ROOT/ClipmanServer/Manual.html" "$PACKAGE_ROOT/Manual.html"
cp "$ROOT/ClipmanServer/clipman-server-settings.example.jsonc" "$PACKAGE_ROOT/clipman-server-settings.example.jsonc"
cp "$ROOT/LICENSE.txt" "$PACKAGE_ROOT/LICENSE.txt"
WINDOWS_EXE="${CLIPMAN_SERVER_WINDOWS_EXE:-$ROOT/ClipmanServerWindows/dist/Clipman Server.exe}"
cp "$WINDOWS_EXE" "$PACKAGE_ROOT/Windows/Clipman Server.exe"

cat > "$PACKAGE_ROOT/Linux/run-clipman-server.sh" <<'SH'
#!/usr/bin/env sh
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$SCRIPT_DIR/.."
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7*) ARCH=armv7 ;;
  *) echo "Unsupported Linux architecture: $(uname -m)" >&2; exit 1 ;;
esac
exec "$SCRIPT_DIR/clipman-server-$ARCH" "$@"
SH
chmod +x "$PACKAGE_ROOT/Linux/run-clipman-server.sh"

MAC_APP="${CLIPMAN_SERVER_MAC_APP:-$ROOT/ClipmanServerMac/dist/Clipman Server.app}"
if [[ ! -d "$MAC_APP" ]]; then
  echo "Mac Clipman Server app is missing. Run ClipmanServerMac/Scripts/package-release.sh first." >&2
  exit 1
fi
COPYFILE_DISABLE=1 ditto --norsrc "$MAC_APP" "$PACKAGE_ROOT/macOS/Clipman Server.app"

cat > "$PACKAGE_ROOT/manifest.json" <<JSON
{
  "Name": "Clipman Server",
  "Version": "$VERSION",
  "ServerProgram": "Linux/run-clipman-server.sh",
  "Platforms": [
    "Linux",
    "macOS",
    "Windows"
  ],
  "Documentation": [
    "Manual.html",
    "clipman-server-settings.example.jsonc"
  ],
  "Dockerfile": "Docker\\\\Dockerfile",
  "WindowsApp": "Windows\\\\Clipman Server.exe",
  "MacApp": "macOS\\\\Clipman Server.app"
}
JSON

sha256_file() { shasum -a 256 "$1" | awk '{print $1}'; }
cat > "$PACKAGE_ROOT/manifest-v2.json" <<JSON
{
  "format_version": 2,
  "name": "Clipman Server",
  "version": "$VERSION",
  "artifacts": [
    {"os":"windows","architecture":"amd64","path":"Windows/Clipman Server.exe","sha256":"$(sha256_file "$PACKAGE_ROOT/Windows/Clipman Server.exe")","executable":true},
    {"os":"darwin","architecture":"universal","path":"macOS/Clipman Server.app/Contents/MacOS/Clipman Server","sha256":"$(sha256_file "$PACKAGE_ROOT/macOS/Clipman Server.app/Contents/MacOS/Clipman Server")","executable":true},
    {"os":"linux","architecture":"amd64","path":"Linux/clipman-server-amd64","sha256":"$(sha256_file "$PACKAGE_ROOT/Linux/clipman-server-amd64")","executable":true},
    {"os":"linux","architecture":"arm64","path":"Linux/clipman-server-arm64","sha256":"$(sha256_file "$PACKAGE_ROOT/Linux/clipman-server-arm64")","executable":true},
    {"os":"linux","architecture":"arm","path":"Linux/clipman-server-armv7","sha256":"$(sha256_file "$PACKAGE_ROOT/Linux/clipman-server-armv7")","executable":true}
  ]
}
JSON

rm -f "$ZIP"
find "$PACKAGE_ROOT" \( -name '._*' -o -name '.DS_Store' \) -delete
COPYFILE_DISABLE=1 ditto -c -k --norsrc --keepParent "$PACKAGE_ROOT" "$ZIP"
echo "Built $ZIP"
