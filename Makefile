.PHONY: build run test clean docker-build docker-run lint fmt

# Binary name
BINARY_NAME=opensearch-query-exporter
DOCKER_IMAGE=opensearch-query-exporter:latest

# Build the binary
build:
	go build -o $(BINARY_NAME) cmd/exporter/main.go

# Run the binary locally
run: build
	./$(BINARY_NAME) -config configs/example-config.yaml

# Run tests
test:
	go test -v ./...

# Clean build artifacts
clean:
	go clean
	rm -f $(BINARY_NAME)

# Build Docker image
docker-build:
	docker build -t $(DOCKER_IMAGE) .

# Run with docker-compose
docker-run:
	docker-compose up -d

# Stop docker-compose
docker-stop:
	docker-compose down

# View logs
docker-logs:
	docker-compose logs -f

# Format code
fmt:
	go fmt ./...

# Run linter
lint:
	golangci-lint run

# Install dependencies
deps:
	go mod download
	go mod tidy

# Build for multiple platforms
build-all:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY_NAME)-linux-amd64 cmd/exporter/main.go
	GOOS=darwin GOARCH=amd64 go build -o $(BINARY_NAME)-darwin-amd64 cmd/exporter/main.go
	GOOS=windows GOARCH=amd64 go build -o $(BINARY_NAME)-windows-amd64.exe cmd/exporter/main.go
