# Build configuration parameters
BINARY_NAME=swiftpos_sync
MODULE_NAME=swiftpos-sync
OUTPUT_DIR=build

.PHONY: all build-linux build-windows clean help

all: build-linux build-windows ## Compile binaries for all core production platforms

build-linux: ## Cross-compile stripped static binary for Linux environments
	@echo "Initializing Linux AMD64 compilation pass..."
	@mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o $(OUTPUT_DIR)/$(BINARY_NAME)_linux_amd64 .
	@echo "Linux binary generated successfully: $(OUTPUT_DIR)/$(BINARY_NAME)_linux_amd64"

build-windows: ## Cross-compile stripped static binary for Windows x64 Host
	@echo "Initializing Windows AMD64 compilation pass..."
	@mkdir -p $(OUTPUT_DIR) 2>/dev/null || mkdir $(OUTPUT_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o $(OUTPUT_DIR)/$(BINARY_NAME).exe .
	@echo "Windows production binary generated successfully: $(OUTPUT_DIR)/$(BINARY_NAME).exe"

clean: ## Purge build outputs and local caches
	@echo "Cleaning up build directory artifacts..."
	@rm -rf $(OUTPUT_DIR) 2>/dev/null || rmdir /s /q $(OUTPUT_DIR) 2>nul || true
	@go clean

help: ## Show this interactive help contract documentation
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
