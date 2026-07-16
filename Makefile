.PHONY: build test vet fmt install clean release-artifacts

build:
	go build -o bin/devhost ./cmd/devhost

# Cross-compiled release tarballs in dist/ (darwin/linux x amd64/arm64)
release-artifacts:
	rm -rf dist && mkdir -p dist
	for os in darwin linux; do for arch in amd64 arm64; do \
		GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "-s -w" -o dist/devhost ./cmd/devhost; \
		tar -czf dist/devhost_$${os}_$${arch}.tar.gz -C dist devhost; \
		rm dist/devhost; \
	done; done
	cd dist && shasum -a 256 *.tar.gz > checksums.txt

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
