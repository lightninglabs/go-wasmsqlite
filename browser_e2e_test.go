//go:build !js

package wasmsqlite

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestBrowserE2E(t *testing.T) {
	if os.Getenv("WASM_BROWSER_TEST") != "1" {
		t.Skip("set WASM_BROWSER_TEST=1 to run browser E2E tests")
	}

	tmpDir := t.TempDir()
	if err := buildWASMTestBinary(tmpDir); err != nil {
		t.Fatalf("build wasm test binary: %v", err)
	}
	if err := copyBrowserTestAssets(tmpDir); err != nil {
		t.Fatalf("copy browser test assets: %v", err)
	}
	if err := writeBrowserTestIndex(tmpDir); err != nil {
		t.Fatalf("write browser test index: %v", err)
	}

	server, url, err := serveBrowserTestDir(tmpDir)
	if err != nil {
		t.Fatalf("start browser test server: %v", err)
	}
	defer server.Shutdown(context.Background())

	allocCtx, cancel := newBrowserAllocator()
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	var consoleLines []string
	chromedp.ListenTarget(ctx, func(event interface{}) {
		if ev, ok := event.(*cdpruntime.EventConsoleAPICalled); ok {
			var parts []string
			for _, arg := range ev.Args {
				if arg.Value != nil {
					parts = append(parts, string(arg.Value))
				}
			}
			if len(parts) > 0 {
				consoleLines = append(consoleLines, strings.Join(parts, " "))
			}
		}
	})

	ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	var passed bool
	var done bool
	var failure string
	timeout := browserTestTimeout(90 * time.Second)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Poll(`window.__wasmTestDone === true`, &done, chromedp.WithPollingInterval(250*time.Millisecond), chromedp.WithPollingTimeout(timeout)),
		chromedp.Evaluate(`window.__wasmTestPassed === true`, &passed),
		chromedp.Evaluate(`window.__wasmTestFailure || ""`, &failure),
	); err != nil {
		t.Fatalf("run browser test: %v\nconsole:\n%s", err, strings.Join(consoleLines, "\n"))
	}

	if !done || !passed {
		t.Fatalf("browser wasm tests failed: %s\nconsole:\n%s", failure, strings.Join(consoleLines, "\n"))
	}
	if testing.Verbose() && len(consoleLines) > 0 {
		t.Logf("browser console:\n%s", strings.Join(consoleLines, "\n"))
	}
}

func TestBrowserE2EExampleSmoke(t *testing.T) {
	if os.Getenv("WASM_BROWSER_TEST") != "1" {
		t.Skip("set WASM_BROWSER_TEST=1 to run browser E2E tests")
	}
	if _, err := os.Stat(filepath.Join("example", "main.wasm")); err != nil {
		t.Skip("example/main.wasm is missing; run make build-example")
	}

	server, url, err := serveBrowserTestDir("example")
	if err != nil {
		t.Fatalf("start example server: %v", err)
	}
	defer server.Shutdown(context.Background())

	allocCtx, cancel := newBrowserAllocator()
	defer cancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	var consoleLines []string
	chromedp.ListenTarget(ctx, func(event interface{}) {
		if ev, ok := event.(*cdpruntime.EventConsoleAPICalled); ok {
			var parts []string
			for _, arg := range ev.Args {
				if arg.Value != nil {
					parts = append(parts, string(arg.Value))
				}
			}
			if len(parts) > 0 {
				consoleLines = append(consoleLines, strings.Join(parts, " "))
			}
		}
	})

	var ready bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		chromedp.Poll(`typeof window.runDemo === "function"`, &ready, chromedp.WithPollingInterval(250*time.Millisecond), chromedp.WithPollingTimeout(60*time.Second)),
	); err != nil {
		t.Fatalf("example app did not initialize: %v\nconsole:\n%s", err, strings.Join(consoleLines, "\n"))
	}
	if !ready {
		t.Fatalf("example app did not expose runDemo")
	}
}

func TestBrowserE2EMultiContextOPFS(t *testing.T) {
	if os.Getenv("WASM_BROWSER_TEST") != "1" {
		t.Skip("set WASM_BROWSER_TEST=1 to run browser E2E tests")
	}

	tmpDir := t.TempDir()
	if err := copyBrowserTestAssets(tmpDir); err != nil {
		t.Fatalf("copy browser test assets: %v", err)
	}
	if err := writeBrowserProbeIndex(tmpDir); err != nil {
		t.Fatalf("write browser probe index: %v", err)
	}

	server, url, err := serveBrowserTestDir(tmpDir)
	if err != nil {
		t.Fatalf("start browser test server: %v", err)
	}
	defer server.Shutdown(context.Background())

	allocCtx, cancel := newBrowserAllocator()
	defer cancel()

	page1, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	page1, cancel = context.WithTimeout(page1, 60*time.Second)
	defer cancel()

	var first browserProbeResult
	if err := chromedp.Run(page1,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		evaluateAwait(`window.openWasmsqliteProbe("/shared-multitab.db", true)`, &first),
	); err != nil {
		t.Fatalf("open first page db: %v", err)
	}
	if !first.OK {
		t.Fatalf("first page did not open OPFS db: %+v", first)
	}

	page2, cancel := chromedp.NewContext(page1)
	defer cancel()
	page2, cancel = context.WithTimeout(page2, 60*time.Second)
	defer cancel()

	var second browserProbeResult
	if err := chromedp.Run(page2,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		evaluateAwait(`window.openWasmsqliteProbe("/shared-multitab.db", true)`, &second),
	); err != nil {
		t.Fatalf("open second page db: %v", err)
	}
	if !second.OK && second.Error == "" {
		t.Fatalf("second page failure is missing an actionable error: %+v", second)
	}
}

func TestBrowserE2EIsolatedBrowserContextOPFS(t *testing.T) {
	if os.Getenv("WASM_BROWSER_TEST") != "1" {
		t.Skip("set WASM_BROWSER_TEST=1 to run browser E2E tests")
	}

	tmpDir := t.TempDir()
	if err := copyBrowserTestAssets(tmpDir); err != nil {
		t.Fatalf("copy browser test assets: %v", err)
	}
	if err := writeBrowserProbeIndex(tmpDir); err != nil {
		t.Fatalf("write browser probe index: %v", err)
	}

	server, url, err := serveBrowserTestDir(tmpDir)
	if err != nil {
		t.Fatalf("start browser test server: %v", err)
	}
	defer server.Shutdown(context.Background())

	profileDir, err := os.MkdirTemp("", "wasmsqlite-chrome-profile-*")
	if err != nil {
		t.Fatalf("create chrome profile dir: %v", err)
	}
	allocCtx, cancel := newBrowserAllocator(
		chromedp.Flag("incognito", true),
		chromedp.UserDataDir(profileDir),
	)
	defer func() {
		cancel()
		time.Sleep(250 * time.Millisecond)
		_ = os.RemoveAll(profileDir)
	}()

	isolatedCtx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	isolatedCtx, cancel = context.WithTimeout(isolatedCtx, 60*time.Second)
	defer cancel()

	var result browserProbeResult
	if err := chromedp.Run(isolatedCtx,
		chromedp.Navigate(url),
		chromedp.WaitReady("body"),
		evaluateAwait(`window.openWasmsqliteProbe("/isolated-context.db", false)`, &result),
	); err != nil {
		t.Fatalf("open isolated-context db: %v", err)
	}
	if !result.OK && result.Error == "" {
		t.Fatalf("isolated browser context failure is missing an actionable error: %+v", result)
	}

	var required browserProbeResult
	if err := chromedp.Run(isolatedCtx,
		evaluateAwait(`window.openWasmsqliteProbe("/isolated-context-required.db", true)`, &required),
	); err != nil {
		t.Fatalf("open isolated-context require-persistent db: %v", err)
	}
	if !required.OK && required.Error == "" {
		t.Fatalf("isolated browser context require-persistent failure is missing an actionable error: %+v", required)
	}
}

type browserProbeResult struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error"`
	VFSType    string `json:"vfsType"`
	Persistent bool   `json:"persistent"`
	Filename   string `json:"filename"`
}

func evaluateAwait(expression string, res any) chromedp.Action {
	return chromedp.Evaluate(expression, res, func(p *cdpruntime.EvaluateParams) *cdpruntime.EvaluateParams {
		return p.WithAwaitPromise(true)
	})
}

func newBrowserAllocator(extra ...chromedp.ExecAllocatorOption) (context.Context, context.CancelFunc) {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(60*time.Second),
	)
	if chromePath := os.Getenv("CHROME_PATH"); chromePath != "" {
		opts = append(opts, chromedp.ExecPath(chromePath))
	}
	opts = append(opts, extra...)

	return chromedp.NewExecAllocator(context.Background(), opts...)
}

func browserTestTimeout(defaultTimeout time.Duration) time.Duration {
	raw := os.Getenv("WASMSQLITE_BROWSER_TEST_TIMEOUT")
	if raw == "" {
		return defaultTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil {
		return defaultTimeout
	}
	return timeout
}

func buildWASMTestBinary(destDir string) error {
	out := filepath.Join(destDir, "driver.test.wasm")
	cmd := exec.Command("go", "test", "-c", "-o", out, ".")
	cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w\n%s", err, output)
	}
	return nil
}

func copyBrowserTestAssets(destDir string) error {
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return err
	}

	files := map[string]string{
		filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"): "wasm_exec.js",
		"assets/sqlite3.js":                  "sqlite3.js",
		"assets/sqlite3.wasm":                "sqlite3.wasm",
		"assets/sqlite3-opfs-async-proxy.js": "sqlite3-opfs-async-proxy.js",
		"bridge/sqlite-bridge.js":            "sqlite-bridge.js",
		"bridge/sqlite-worker.js":            "sqlite-worker.js",
	}

	for src, dst := range files {
		if err := copyFile(src, filepath.Join(destDir, dst)); err != nil {
			return fmt.Errorf("copy %s: %w", src, err)
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func writeBrowserTestIndex(destDir string) error {
	const index = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>wasmsqlite browser tests</title></head>
<body>
<script src="sqlite-bridge.js"></script>
<script src="wasm_exec.js"></script>
<script>
window.__wasmTestDone = false;
window.__wasmTestPassed = false;
window.__wasmTestFailure = "";
(async () => {
  try {
    const go = new Go();
    go.env = {
      WASMSQLITE_HUGE_BATCH_TEST: new URLSearchParams(window.location.search).get("hugeBatch") || ""
    };
    const result = await WebAssembly.instantiateStreaming(fetch("driver.test.wasm"), go.importObject);
    await go.run(result.instance);
  } catch (error) {
    console.error("wasm test bootstrap failed", error);
    window.__wasmTestFailure = error && (error.stack || error.message) || String(error);
    window.__wasmTestDone = true;
    window.__wasmTestPassed = false;
  }
})();
</script>
</body>
</html>
`
	return os.WriteFile(filepath.Join(destDir, "index.html"), []byte(index), 0644)
}

func writeBrowserProbeIndex(destDir string) error {
	const index = `<!doctype html>
<html>
<head><meta charset="utf-8"><title>wasmsqlite browser probe</title></head>
<body>
<script src="sqlite-bridge.js"></script>
<script>
window.openWasmsqliteProbe = async (file, requirePersistent) => {
  try {
    const result = await window.sqliteBridge.open({
      file,
      vfs: "opfs",
      busyTimeout: 1000,
      requirePersistent: !!requirePersistent
    });
    return {
      ok: true,
      vfsType: result.vfsType,
      persistent: !!result.persistent,
      filename: result.filename
    };
  } catch (error) {
    return {
      ok: false,
      error: error && (error.message || error.stack) || String(error)
    };
  }
};
</script>
</body>
</html>
`
	return os.WriteFile(filepath.Join(destDir, "index.html"), []byte(index), 0644)
}

func serveBrowserTestDir(dir string) (*http.Server, string, error) {
	return serveBrowserTestDirWithHeaders(dir, true)
}

func serveBrowserTestDirWithHeaders(dir string, crossOriginHeaders bool) (*http.Server, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", err
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if crossOriginHeaders {
			w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
			w.Header().Set("Cross-Origin-Embedder-Policy", "require-corp")
			w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
		}
		if filepath.Ext(r.URL.Path) == ".wasm" {
			w.Header().Set("Content-Type", "application/wasm")
		}
		http.FileServer(http.Dir(dir)).ServeHTTP(w, r)
	})

	server := &http.Server{
		Handler:  handler,
		ErrorLog: log.New(io.Discard, "", 0),
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			panic(err)
		}
	}()

	url := "http://" + listener.Addr().String() + "/"
	if os.Getenv("WASMSQLITE_HUGE_BATCH_TEST") == "1" {
		url += "?hugeBatch=1"
	}
	return server, url, nil
}
