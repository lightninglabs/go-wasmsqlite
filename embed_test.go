package wasmsqlite

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEmbeddedAssets(t *testing.T) {
	// Test listing assets
	assets, err := ListAssets()
	if err != nil {
		t.Fatalf("Failed to list assets: %v", err)
	}

	// Check we have the expected files
	expectedFiles := []string{
		"assets/sqlite3.wasm",
		"assets/sqlite3.js",
		"assets/sqlite3-worker1.js",
		"assets/sqlite3-worker1-promiser.js",
		"assets/sqlite3-opfs-async-proxy.js",
		"bridge/sqlite-bridge.js",
	}

	for _, expected := range expectedFiles {
		found := false
		for _, asset := range assets {
			if asset == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected asset not found: %s", expected)
		}
	}

	// Test getting individual assets
	wasm, err := GetSQLiteWASM()
	if err != nil {
		t.Errorf("Failed to get SQLite WASM: %v", err)
	}
	if len(wasm) == 0 {
		t.Error("SQLite WASM is empty")
	}

	js, err := GetSQLiteJS()
	if err != nil {
		t.Errorf("Failed to get SQLite JS: %v", err)
	}
	if len(js) == 0 {
		t.Error("SQLite JS is empty")
	}

	bridge, err := GetBridgeJS()
	if err != nil {
		t.Errorf("Failed to get Bridge JS: %v", err)
	}
	if len(bridge) == 0 {
		t.Error("Bridge JS is empty")
	}
}

func TestExtractAssets(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "sqlc-wasm-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract assets
	err = ExtractAssets(tmpDir)
	if err != nil {
		t.Fatalf("Failed to extract assets: %v", err)
	}

	// Verify files exist
	expectedFiles := []string{
		"assets/sqlite3.wasm",
		"assets/sqlite3.js",
		"bridge/sqlite-bridge.js",
	}

	for _, file := range expectedFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file not extracted: %s", file)
		}
	}
}
