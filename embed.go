package wasmsqlite

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Embed all assets and bridge files
//
//go:embed assets/* bridge/sqlite-bridge.js bridge/sqlite-worker.js
var embeddedAssets embed.FS

var runtimeAssetPaths = map[string]string{
	"sqlite3.wasm":                "assets/sqlite3.wasm",
	"sqlite3.js":                  "assets/sqlite3.js",
	"sqlite3-opfs-async-proxy.js": "assets/sqlite3-opfs-async-proxy.js",
	"sqlite-bridge.js":            "bridge/sqlite-bridge.js",
	"sqlite-worker.js":            "bridge/sqlite-worker.js",
}

// ExtractAssets extracts all embedded SQLite WASM assets to the specified
// directory using the flat layout expected by the browser runtime:
// sqlite3.js, sqlite3.wasm, sqlite3-opfs-async-proxy.js, sqlite-bridge.js, and
// sqlite-worker.js.
//
// Example:
//
//	err := wasmsqlite.ExtractAssets("./static/wasm")
//	if err != nil {
//	    log.Fatal(err)
//	}
func ExtractAssets(destDir string) error {
	for filename, embeddedPath := range runtimeAssetPaths {
		data, err := embeddedAssets.ReadFile(embeddedPath)
		if err != nil {
			return fmt.Errorf("reading embedded file %s: %w", embeddedPath, err)
		}

		destPath := filepath.Join(destDir, filename)

		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", destPath, err)
		}

		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("writing file %s: %w", destPath, err)
		}
	}

	return nil
}

// AssetHandler returns an http.Handler that serves the embedded runtime assets
// from flat paths such as /sqlite3.js and /sqlite-worker.js. It also preserves
// access to the embedded paths under /assets/ and /bridge/ for compatibility.
//
// The handler sets cross-origin isolation headers on asset responses. The app
// page itself must also be served with these headers for OPFS to work; use
// WithCrossOriginIsolation to wrap the whole app handler.
//
// Example:
//
//	handler := wasmsqlite.AssetHandler()
//	http.Handle("/wasm/", http.StripPrefix("/wasm", handler))
func AssetHandler() http.Handler {
	fileServer := http.FileServer(http.FS(embeddedAssets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCrossOriginIsolationHeaders(w)
		path := strings.TrimPrefix(r.URL.Path, "/")
		if embeddedPath, ok := runtimeAssetPaths[path]; ok {
			if filepath.Ext(path) == ".wasm" {
				w.Header().Set("Content-Type", "application/wasm")
			}
			http.ServeFileFS(w, r, embeddedAssets, embeddedPath)
			return
		}

		if filepath.Ext(path) == ".wasm" {
			w.Header().Set("Content-Type", "application/wasm")
		}

		fileServer.ServeHTTP(w, r)
	})
}

// WithCrossOriginIsolation wraps a handler and sets the headers required by
// SharedArrayBuffer-backed OPFS. Wrap the app page handler with this, not only
// the static SQLite asset handler.
func WithCrossOriginIsolation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setCrossOriginIsolationHeaders(w)
		if filepath.Ext(r.URL.Path) == ".wasm" {
			w.Header().Set("Content-Type", "application/wasm")
		}
		next.ServeHTTP(w, r)
	})
}

func setCrossOriginIsolationHeaders(w http.ResponseWriter) {
	w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
	w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
}

// GetAsset returns the contents of a specific embedded asset.
//
// Example:
//
//	wasmBytes, err := wasmsqlite.GetAsset("assets/sqlite3.wasm")
//	if err != nil {
//	    log.Fatal(err)
//	}
func GetAsset(path string) ([]byte, error) {
	return embeddedAssets.ReadFile(path)
}

// GetSQLiteWASM returns the SQLite WebAssembly binary.
func GetSQLiteWASM() ([]byte, error) {
	return GetAsset("assets/sqlite3.wasm")
}

// GetSQLiteJS returns the SQLite JavaScript wrapper.
func GetSQLiteJS() (string, error) {
	data, err := GetAsset("assets/sqlite3.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetBridgeJS returns the main-thread JavaScript RPC bridge.
func GetBridgeJS() (string, error) {
	data, err := GetAsset("bridge/sqlite-bridge.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GetWorkerJS returns the dedicated SQLite worker JavaScript.
func GetWorkerJS() (string, error) {
	data, err := GetAsset("bridge/sqlite-worker.js")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ListAssets returns a list of all embedded asset paths.
func ListAssets() ([]string, error) {
	var assets []string

	err := fs.WalkDir(embeddedAssets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			assets = append(assets, path)
		}

		return nil
	})

	return assets, err
}

// AssetFS returns the embedded filesystem for direct access.
// This can be useful for custom serving or processing needs.
func AssetFS() fs.FS {
	return embeddedAssets
}
