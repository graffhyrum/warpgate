.PHONY: build test lint run clean docker-build

BINARY := bin/warpgate

build:
	go build -o $(BINARY) ./cmd/warpgate
	go build -o bin/warpgate-tui ./cmd/warpgate-tui

run: build
	./$(BINARY)

test:
	go test -race ./...

lint:
	@which golangci-lint > /dev/null 2>&1 || \
		(echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

vet:
	go vet ./...

clean:
	rm -rf bin/

docker-build:
	docker build -t warpgate .
