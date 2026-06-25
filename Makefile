# Maestro Registry Console

all: build test

## Build the React UI (output → internal/ui/dist/)
ui:
	@echo "Building UI..."
	@cd ui && npm ci && npm run build

## Compile the Go binary (embeds the UI — run `make ui` first)
build:
	@echo "Building maestro..."
	@go build -o maestro.exe ./cmd/maestro

## Build UI then binary
release: ui build

run:
	@go run ./cmd/maestro

## Start Vite dev server (proxies /api to :8080 — run `make run` in parallel)
ui-dev:
	@cd ui && npm run dev

test:
	@echo "Testing..."
	@go test ./... -v

clean:
	@echo "Cleaning..."
	@rm -f maestro.exe
	@rm -rf internal/ui/dist
	@mkdir -p internal/ui/dist
	@echo "<p>UI not built — run: make ui</p>" > internal/ui/dist/index.html
watch:
	@powershell -ExecutionPolicy Bypass -Command "if (Get-Command air -ErrorAction SilentlyContinue) { \
		air; \
		Write-Output 'Watching...'; \
	} else { \
		Write-Output 'Installing air...'; \
		go install github.com/air-verse/air@latest; \
		air; \
		Write-Output 'Watching...'; \
	}"

.PHONY: all build release run ui ui-dev test clean watch
