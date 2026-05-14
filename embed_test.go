//go:build !js

package wasmsqlite

import (
	"net/http"
	"net/http/httptest"
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
		"assets/sqlite3-opfs-async-proxy.js",
		"bridge/sqlite-bridge.js",
		"bridge/sqlite-worker.js",
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

	worker, err := GetWorkerJS()
	if err != nil {
		t.Errorf("Failed to get worker JS: %v", err)
	}
	if len(worker) == 0 {
		t.Error("Worker JS is empty")
	}
}

func TestExtractAssets(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "go-wasmsqlite-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Extract assets
	err = ExtractAssets(tmpDir)
	if err != nil {
		t.Fatalf("Failed to extract assets: %v", err)
	}

	// Verify files exist in the flat runtime layout expected by the JS bridge.
	expectedFiles := []string{
		"sqlite3.wasm",
		"sqlite3.js",
		"sqlite3-opfs-async-proxy.js",
		"sqlite-bridge.js",
		"sqlite-worker.js",
	}

	for _, file := range expectedFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Expected file not extracted: %s", file)
		}
	}
}

func TestAssetHandlerServesFlatRuntimePaths(t *testing.T) {
	server := httptest.NewServer(AssetHandler())
	defer server.Close()

	resp, err := http.Get(server.URL + "/sqlite3.js")
	if err != nil {
		t.Fatalf("get sqlite3.js: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cross-Origin-Opener-Policy"); got != "same-origin" {
		t.Fatalf("unexpected COOP header: %q", got)
	}
	if got := resp.Header.Get("Cross-Origin-Embedder-Policy"); got != "require-corp" {
		t.Fatalf("unexpected COEP header: %q", got)
	}
}
