#!/bin/bash
set -e

# Configuration
SQLITE_VERSION="3500400"
SQLITE_URL="https://www.sqlite.org/2025/sqlite-wasm-${SQLITE_VERSION}.zip"
EXPECTED_SHA="cdff32cba45537d96efd883a9a3dd09af7616b2fa9b414afbbc7a36fbe474af5"
ASSETS_DIR="assets"
TEMP_DIR="temp_sqlite_download"

echo "📦 Fetching SQLite WASM ${SQLITE_VERSION}..."

# Create assets directory if it doesn't exist
mkdir -p "$ASSETS_DIR"

# Create temp directory
rm -rf "$TEMP_DIR"
mkdir -p "$TEMP_DIR"

# Download the zip file
echo "⬇️  Downloading from $SQLITE_URL..."
curl -L -o "$TEMP_DIR/sqlite-wasm.zip" "$SQLITE_URL"

# Verify SHA3-256 checksum
echo "🔐 Verifying checksum..."
if command -v sha3sum >/dev/null 2>&1; then
    ACTUAL_SHA=$(sha3sum -a 256 "$TEMP_DIR/sqlite-wasm.zip" | cut -d' ' -f1)
elif command -v openssl >/dev/null 2>&1; then
    ACTUAL_SHA=$(openssl dgst -sha3-256 "$TEMP_DIR/sqlite-wasm.zip" | sed 's/^.*= //')
else
    echo "⚠️  Warning: Neither sha3sum nor openssl found. Skipping checksum verification."
    ACTUAL_SHA=""
fi

if [ -n "$ACTUAL_SHA" ]; then
    if [ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]; then
        echo "❌ Checksum mismatch!"
        echo "   Expected: $EXPECTED_SHA"
        echo "   Got:      $ACTUAL_SHA"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    echo "✅ Checksum verified"
fi

# Extract the zip file
echo "📂 Extracting files..."
unzip -q "$TEMP_DIR/sqlite-wasm.zip" -d "$TEMP_DIR"

# Copy only the required files
echo "📋 Copying required files to $ASSETS_DIR..."
cp "$TEMP_DIR/sqlite-wasm-${SQLITE_VERSION}/jswasm/sqlite3.js" "$ASSETS_DIR/"
cp "$TEMP_DIR/sqlite-wasm-${SQLITE_VERSION}/jswasm/sqlite3.wasm" "$ASSETS_DIR/"
cp "$TEMP_DIR/sqlite-wasm-${SQLITE_VERSION}/jswasm/sqlite3-worker1.js" "$ASSETS_DIR/"
cp "$TEMP_DIR/sqlite-wasm-${SQLITE_VERSION}/jswasm/sqlite3-worker1-promiser.js" "$ASSETS_DIR/"
cp "$TEMP_DIR/sqlite-wasm-${SQLITE_VERSION}/jswasm/sqlite3-opfs-async-proxy.js" "$ASSETS_DIR/"

# Clean up
echo "🧹 Cleaning up..."
rm -rf "$TEMP_DIR"

echo "✨ SQLite WASM assets successfully fetched and installed!"
echo "   Files in $ASSETS_DIR:"
ls -la "$ASSETS_DIR"