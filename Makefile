BINARY      := find_duplicates
LDFLAGS     := -trimpath -ldflags="-s -w"
export GOTOOLCHAIN := local
export GOFLAGS     := -mod=mod

.PHONY: build test vet fmt tidy clean

build:
	go build $(LDFLAGS) -o $(BINARY) .
	go mod edit -go=1.23 -toolchain=none

test:
	go test ./...
	go mod edit -go=1.23 -toolchain=none

vet:
	go vet ./...
	go mod edit -go=1.23 -toolchain=none

fmt:
	gofmt -l -w .

tidy:
	go mod tidy
	go mod edit -go=1.23 -toolchain=none

clean:
	rm -f $(BINARY) dryrun_duplicates.txt duplicates.txt skipped_duplicates.txt dot_underscore_files.txt

all: tidy fmt vet test build
