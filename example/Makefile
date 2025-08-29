.PHONY: build serve clean

# Build the WebAssembly binary
build:
	GOOS=js GOARCH=wasm go build -o main.wasm ./main.go
	cp $(shell go env GOROOT)/lib/wasm/wasm_exec.js .
	cp ../assets/sqlite3.wasm .
	cp ../assets/bridge.worker.js .

# Serve the demo locally (requires Node.js)
serve: build
	@echo "🌐 Starting local server at http://localhost:8081"
	@echo "✅ OPFS support enabled on localhost"
	@echo "✅ CORS headers configured for SharedArrayBuffer"
	@echo "⚠️  For production, use HTTPS for OPFS support!"
	node server.js

# Alternative: serve with Python (no CORS headers)
serve-python: build
	@echo "🌐 Starting Python server at http://localhost:8081"
	@echo "⚠️  Note: Python server doesn't set CORS headers"
	python3 -m http.server 8081 --bind localhost

# Clean build artifacts
clean:
	rm -f main.wasm wasm_exec.js sqlite3.wasm bridge.worker.js

# Check requirements
check:
	@echo "Checking requirements..."
	@go version
	@python3 --version
	@echo "✅ All requirements met!"

help:
	@echo "wasmsqlite Demo Build Commands:"
	@echo ""
	@echo "  make build   - Build the WebAssembly binary"
	@echo "  make serve   - Build and serve the demo locally"
	@echo "  make clean   - Clean build artifacts"
	@echo "  make check   - Check requirements"
	@echo "  make help    - Show this help"