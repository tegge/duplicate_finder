BINARY      := find_duplicates
LDFLAGS     := -trimpath -ldflags="-s -w"
GOVERSION   := 1.23
export GOTOOLCHAIN := local
export GOFLAGS     := -mod=mod

.PHONY: build test vet fmt tidy clean release

build:
	go build $(LDFLAGS) -o $(BINARY) .
	go mod edit -go=$(GOVERSION) -toolchain=none

test:
	go test ./...
	go mod edit -go=$(GOVERSION) -toolchain=none

vet:
	go vet ./...
	go mod edit -go=$(GOVERSION) -toolchain=none

fmt:
	gofmt -l -w .

tidy:
	go mod tidy
	go mod edit -go=$(GOVERSION) -toolchain=none

dist/find_duplicates-darwin-arm64:
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o dist/find_duplicates-darwin-arm64  .

dist/find_duplicates-darwin-amd64:
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o dist/find_duplicates-darwin-amd64  .

dist/find_duplicates-linux-amd64:
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o dist/find_duplicates-linux-amd64   .

dist/find_duplicates-linux-arm64:
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o dist/find_duplicates-linux-arm64   .

release: dist/find_duplicates-darwin-arm64 dist/find_duplicates-darwin-amd64 dist/find_duplicates-linux-amd64 dist/find_duplicates-linux-arm64
	@echo "Binaries in dist/:"
	@ls -lh dist/
	go mod edit -go=$(GOVERSION) -toolchain=none

clean:
	rm -f $(BINARY) dryrun_duplicates.txt duplicates.txt skipped_duplicates.txt dot_underscore_files.txt
	rm -rf dist/

all: tidy fmt vet test build
