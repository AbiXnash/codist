# Codist builds as one file with no C compiler. Only the Java grammar is embedded, which keeps the
# binary small; every build and test must use the same tags.
TAGS := grammar_subset grammar_subset_java
GO   := CGO_ENABLED=0 go

.PHONY: build test run dist clean

build:
	$(GO) build -tags '$(TAGS)' -o bin/codist .

test:
	$(GO) test -tags '$(TAGS)' ./...

run: build
	./bin/codist $(REPO)

# One binary per system, ready to copy.
dist:
	GOOS=darwin  GOARCH=arm64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-x64 .
	GOOS=linux   GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-linux-x64 .
	GOOS=windows GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-windows-x64.exe .

clean:
	rm -rf bin dist
