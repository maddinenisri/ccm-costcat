BIN := ccm-costcat

.PHONY: build test vet run clean

build:
	go build -o bin/$(BIN) ./cmd/$(BIN)

test:
	go test ./...

vet:
	go vet ./...

run: build
	./bin/$(BIN) list -stdout

clean:
	rm -rf bin out
