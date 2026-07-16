.PHONY: build test vet fmt install clean snapshot

build:
	go build -o bin/devhost ./cmd/devhost

# Local dry-run of the release pipeline (artifacts in dist/, nothing published).
# Real releases: push a v* tag; .github/workflows/release.yml runs goreleaser.
snapshot:
	goreleaser release --snapshot --clean

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
