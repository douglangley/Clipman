#!/usr/bin/env sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
APPLICATIONS_DIR="${CLIPMAN_APPLICATIONS_DIR:-$HOME/Applications}"
BIN_DIR="${CLIPMAN_BIN_DIR:-$HOME/.local/bin}"
SOURCE_APP="$SCRIPT_DIR/Clipman Server.app"
TARGET_APP="$APPLICATIONS_DIR/Clipman Server.app"

for required in "$SCRIPT_DIR/clipmanserver" "$SOURCE_APP/Contents/MacOS/Clipman Server" "$SOURCE_APP/Contents/Resources/clipman-server"; do
  [ -e "$required" ] || { echo "Required release file is missing: $required" >&2; exit 1; }
done

case "$APPLICATIONS_DIR:$BIN_DIR" in
  /*:/*) ;;
  *) echo "Installation directories must be absolute paths." >&2; exit 2 ;;
esac
if [ "$APPLICATIONS_DIR" = "/" ] || [ "$BIN_DIR" = "/" ]; then
  echo "Installation directories cannot be the filesystem root." >&2
  exit 2
fi
case "$TARGET_APP" in
  *"'"*|*"
"*) echo "The application path cannot contain a single quote or newline." >&2; exit 2 ;;
esac

mkdir -p "$APPLICATIONS_DIR" "$BIN_DIR"
stage="$APPLICATIONS_DIR/.Clipman Server.install.$$"
backup="$APPLICATIONS_DIR/.Clipman Server.backup.$$"
rm -rf "$stage" "$backup"

cleanup() {
  rm -rf "$stage"
}
trap cleanup EXIT HUP INT TERM

if command -v ditto >/dev/null 2>&1; then
  COPYFILE_DISABLE=1 ditto --norsrc "$SOURCE_APP" "$stage"
else
  cp -R "$SOURCE_APP" "$stage"
fi

if [ -e "$TARGET_APP" ]; then
  mv "$TARGET_APP" "$backup"
fi
if ! mv "$stage" "$TARGET_APP"; then
  [ ! -e "$backup" ] || mv "$backup" "$TARGET_APP"
  echo "Could not install Clipman Server.app; the previous app was restored." >&2
  exit 1
fi
rm -rf "$backup"

cat > "$BIN_DIR/clipmanserver" <<EOF
#!/usr/bin/env sh
set -eu
APP='$TARGET_APP'
if [ "\$#" -eq 0 ]; then exec open "\$APP"; fi
exec "\$APP/Contents/Resources/clipman-server" "\$@"
EOF
chmod 755 "$BIN_DIR/clipmanserver"

echo "Installed Clipman Server.app in $APPLICATIONS_DIR"
echo "Installed clipmanserver in $BIN_DIR"
