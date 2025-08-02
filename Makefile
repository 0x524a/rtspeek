BINARY=rtspeek

.PHONY: build test vet run

build:
	go build -o bin/$(BINARY) ./cmd/rtspeek

run:
	go run ./cmd/rtspeek --url $(URL)

test:
	go test ./...

vet:
	go vet ./...
