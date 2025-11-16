APP_NAME := pr-manager
MAIN_PKG := ./cmd/main.go
COVER_PROFILE := coverage.out
COVER_HTML := coverage.html

ifeq ($(OS),Windows_NT)
	BIN := $(APP_NAME).exe
else
	BIN := $(APP_NAME)
endif

.PHONY: build run test cover cover-html fmt tidy lint clean

build: ## Build the application binary.
	go build -o $(BIN) $(MAIN_PKG)

run: ## Run the application with go run.
	go run $(MAIN_PKG)

test: ## Run the full unit-test suite.
	go test ./...

cover: ## Generate coverage profile and print totals.
	go test -coverprofile=$(COVER_PROFILE) ./...
	go tool cover -func=$(COVER_PROFILE)

cover-html: cover ## Build an HTML coverage report (depends on cover target).
	go tool cover -html=$(COVER_PROFILE) -o $(COVER_HTML)
	@echo "HTML coverage report: $(COVER_HTML)"

fmt: ## Format all Go sources.
	go fmt ./...

tidy: ## Ensure go.mod/go.sum are in sync.
	go mod tidy

lint: ## Run static analysis via golangci-lint.
	golangci-lint run ./...

clean: ## Clean build artifacts and coverage files.
	go clean ./...
	@rm -f $(BIN) $(COVER_PROFILE) $(COVER_HTML)
