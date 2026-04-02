.PHONY: build clean test run install fmt lint

BINARY=claude
BUILD_DIR=./bin

build:
	@mkdir -p $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/claude

clean:
	rm -rf $(BUILD_DIR)

test:
	go test -v ./...

run: build
	./$(BUILD_DIR)/$(BINARY)

install: build
	cp $(BUILD_DIR)/$(BINARY) $(GOPATH)/bin/

fmt:
	go fmt ./...

lint:
	go vet ./...
