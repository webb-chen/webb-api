#!/usr/bin/env bash
#
# Build the webb-api deploy tarball on Linux/macOS.
# Copies the FULL project source tree so the Docker build context is complete.
#
# Usage:
#   bash deploy/make-package.sh
# Output:
#   deploy/webb-api-deploy-<date>.tar.gz
#
set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

DATE=$(date +%Y%m%d)
PACKAGE_NAME="webb-api-deploy-${DATE}"
PACKAGE_TAR="$SCRIPT_DIR/${PACKAGE_NAME}.tar.gz"
STAGE_DIR="$SCRIPT_DIR/.stage-${PACKAGE_NAME}"
DEST="$STAGE_DIR/$PACKAGE_NAME"

echo "============================================"
echo "  Building webb-api deploy package"
echo "============================================"
echo "Project root: $PROJECT_ROOT"
echo ""

rm -rf "$STAGE_DIR"
mkdir -p "$DEST"

# rsync must exist (standard on Linux/macOS); bail out with a clear message otherwise
if ! command -v rsync >/dev/null 2>&1; then
  echo "ERROR: rsync not found. Install it (e.g. dnf/apt install rsync) and retry." >&2
  exit 1
fi

# Copy the FULL source tree, excluding:
#   - VCS / editor / CI junk, caches, node_modules / dist
#   - non-required trees (docs, electron, e2e, bin)
#   - runtime data/logs
#   - SECRETS: *.key, .license-selftest (contains a private key)
#   - local artifacts: *.exe *.bak *.lic *.tar.gz .env
rsync -a \
  --exclude='.git/' \
  --exclude='.github/' \
  --exclude='.agents/' \
  --exclude='.vscode/' \
  --exclude='.idea/' \
  --exclude='node_modules/' \
  --exclude='dist/' \
  --exclude='.gocache/' \
  --exclude='.eslintcache' \
  --exclude='docs/' \
  --exclude='electron/' \
  --exclude='e2e/' \
  --exclude='bin/' \
  --exclude='.license-selftest/' \
  --exclude='data/' \
  --exclude='logs/' \
  --exclude='.stage-*' \
  --exclude='*.exe' \
  --exclude='*.tar.gz' \
  --exclude='*.bak' \
  --exclude='*.lic' \
  --exclude='*.key' \
  --exclude='*.pem' \
  --exclude='.env' \
  "$PROJECT_ROOT"/ "$DEST"/

# Place deploy.sh at the package root and ensure LF line endings
cp "$SCRIPT_DIR/deploy.sh" "$DEST/deploy.sh"
if command -v sed >/dev/null 2>&1; then
  sed -i 's/\r$//' "$DEST/deploy.sh"
fi
chmod +x "$DEST/deploy.sh"

# Safety check: no private keys inside the package
if find "$DEST" -type f \( -name '*.key' -o -name '*.pem' -o -name 'private.key' \) | grep -q .; then
  echo "WARNING: key-like files found in package:"
  find "$DEST" -type f \( -name '*.key' -o -name '*.pem' -o -name 'private.key' \)
fi

# Compress
echo "Compressing..."
tar czf "$PACKAGE_TAR" -C "$STAGE_DIR" "$PACKAGE_NAME"
rm -rf "$STAGE_DIR"

echo ""
echo "============================================"
echo "  Package ready!"
echo "============================================"
echo ""
echo "  Archive:  $PACKAGE_TAR"
echo "  Size:     $(du -h "$PACKAGE_TAR" | cut -f1)"
echo ""
echo "  Deployment steps:"
echo "    1. Upload:    scp ${PACKAGE_NAME}.tar.gz user@server:/tmp/"
echo "    2. Extract:   cd /tmp && tar xzf ${PACKAGE_NAME}.tar.gz"
echo "    3. Deploy:    cd $PACKAGE_NAME && sudo bash deploy.sh"
echo "    4. Visit:     http://<server-ip>:3000"
echo ""
