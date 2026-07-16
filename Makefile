.PHONY: build test vet fmt install clean

build:
	go build -o bin/devhost ./cmd/devhost

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

install:
	go install ./cmd/devhost

clean:
	rm -rf bin
