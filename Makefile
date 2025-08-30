.PHONY: all build clean test serve help build-wasm build-example setup fetch-assets

# Default target
all: build

# Fetch SQLite WASM assets
fetch-assets:
	@echo "📦 Fetching SQLite WASM assets..."
	@./scripts/fetch-sqlite-wasm.sh

# Build the main WASM module
build-wasm:
	@echo "🔨 Building Go WASM module..."
	GOOS=js GOARCH=wasm go build -v -o main.wasm
	@echo "✅ WASM module built"

# Build example
build-example: fetch-assets build-wasm
	@echo "🔨 Building example..."
	cd example && GOOS=js GOARCH=wasm go build -o main.wasm ./main.go
	cd example && cp $$(go env GOROOT)/lib/wasm/wasm_exec.js .
	@echo "📦 Copying bridge and assets to example..."
	cp bridge/sqlite-bridge.js example/
	cp assets/*.js example/
	cp assets/*.wasm example/
	@echo "✅ Example built"

# Build everything
build: build-example
	@echo "✅ All components built successfully"

# Initial setup
setup: fetch-assets
	@echo "✅ Setup complete"

# Serve the demo locally
serve: build-example
	@echo "🌐 Starting local server at http://localhost:8081"
	@echo "✅ OPFS support enabled"
	@echo "✅ Cross-Origin Isolation enabled"
	cd example && node server.js

# Run tests
test:
	@echo "🧪 Running tests..."
	go test -v ./...
	@echo "✅ Tests passed"

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	rm -f main.wasm
	rm -f example/main.wasm example/wasm_exec.js
	rm -f example/sqlite3.wasm
	rm -rf assets
	@echo "✅ Clean complete"

# Development mode - watch for changes and rebuild
dev:
	@echo "👀 Starting development mode..."
	@echo "Watching for changes in Go and TypeScript files..."
	@make build
	@make serve

# Check requirements
check:
	@echo "🔍 Checking requirements..."
	@go version || (echo "❌ Go not installed" && exit 1)
	@node --version || (echo "❌ Node.js not installed" && exit 1)
	@npm --version || (echo "❌ npm not installed" && exit 1)
	@echo "✅ All requirements met!"

# Help
help:
	@echo "sqlc-wasm Makefile Commands:"
	@echo ""
	@echo "  make setup        - Initial setup (fetch SQLite WASM assets)"
	@echo "  make fetch-assets - Download SQLite WASM from official source"
	@echo "  make build        - Build all components"
	@echo "  make build-wasm   - Build Go WASM module only"
	@echo "  make build-example- Build example only"
	@echo "  make serve        - Build and serve the demo locally"
	@echo "  make test         - Run tests"
	@echo "  make clean        - Clean build artifacts"
	@echo "  make check        - Check requirements"
	@echo "  make help         - Show this help"
	@echo ""
	@echo "Quick start:"
	@echo "  1. make setup"
	@echo "  2. make build"
	@echo "  3. make serve"