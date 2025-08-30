package wasmsqlite

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
)

// Embed all assets and bridge files
//
//go:embed assets/* bridge/sqlite-bridge.js
var embeddedAssets embed.FS

// ExtractAssets extracts all embedded SQLite WASM assets to the specified directory.
// This includes sqlite3.wasm, sqlite3.js, worker files, and the bridge.
//
// Example:
//
//	err := wasmsqlite.ExtractAssets("./static/wasm")
//	if err != nil {
//	    log.Fatal(err)
//	}
func ExtractAssets(destDir string) error {
	return fs.WalkDir(embeddedAssets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if d.IsDir() {
			return nil
		}

		// Read embedded file
		data, err := embeddedAssets.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded file %s: %w", path, err)
		}

		// Create destination path
		destPath := filepath.Join(destDir, path)

		// Create parent directories
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", destPath, err)
		}

		// Write file
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			return fmt.Errorf("writing file %s: %w", destPath, err)
		}

		return nil
	})
}

// AssetHandler returns an http.Handler that serves the embedded assets.
// The handler automatically sets appropriate CORS headers for OPFS support.
//
// Example:
//
//	handler := wasmsqlite.AssetHandler()
//	http.Handle("/wasm/", http.StripPrefix("/wasm", handler))
func AssetHandler() http.Handler {
	fileServer := http.FileServer(http.FS(embeddedAssets))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers for OPFS support
		w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")

		// Set appropriate content types
		if filepath.Ext(r.URL.Path) == ".wasm" {
			w.Header().Set("Content-Type", "application/wasm")
		}

		fileServer.ServeHTTP(w, r)
	})
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

// GetBridgeJS returns the handcrafted JavaScript bridge.
func GetBridgeJS() (string, error) {
	data, err := GetAsset("bridge/sqlite-bridge.js")
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
