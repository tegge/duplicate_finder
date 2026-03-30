BINARY   := find_duplicates
GOFLAGS  := -trimpath -ldflags="-s -w"

.PHONY: build test vet fmt tidy clean

build:
	go build $(GOFLAGS) -o $(BINARY) .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

tidy:
	go mod tidy

clean:
	rm -f $(BINARY) dryrun_duplicates.txt duplicates.txt skipped_duplicates.txt dot_underscore_files.txt

all: tidy fmt vet test build
