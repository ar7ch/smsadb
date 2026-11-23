BIN := smsadb

.PHONY: build test lint fmt clean

build:
	go build -o $(BIN) .

test:
	go test ./...

lint:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "run make fmt"; exit 1; }
	go vet ./...
	golangci-lint run

fmt:
	gofmt -w .

clean:
	rm -f $(BIN)
