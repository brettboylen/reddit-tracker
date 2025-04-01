.PHONY: build run test clean

# Variables
APP_NAME=reddit-tracker
GO_FILES=$(shell find . -name "*.go" -type f -not -path "./vendor/*")
GOPATH=$(shell go env GOPATH)

# Default target
all: build

# Build the app
build:
	@echo "Building $(APP_NAME)..."
	go build -o $(APP_NAME) .

# Run it
run: build
	@echo "Running $(APP_NAME)..."
	./$(APP_NAME)

# Run tests
test:
	@echo "Running tests..."
	go test ./... -v

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Install dependencies
deps:
	@echo "Installing dependencies..."
	go mod tidy

# Check for race conditions
race:
	@echo "Building with race detector..."
	go build -race -o $(APP_NAME) .
	