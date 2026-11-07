# Codist builds as one file with no C compiler. The interface comes built from the codist-ui module, and only the
# Java grammar is embedded in the parser (codist-java), which keeps the binary small.
TAGS := grammar_subset grammar_subset_java
GO   := CGO_ENABLED=0 go

.PHONY: build test run dev dist clean

build:
	$(GO) build -tags '$(TAGS)' -o bin/codist .

test:
	$(GO) test -tags '$(TAGS)' ./...

run: build
	./bin/codist $(REPO)

# Go server on :8080 and the interface with live reload on :5173, from a codist-ui checkout next to this one.
dev:
	@trap 'kill 0' EXIT; $(GO) run -tags '$(TAGS)' . --no-open $(REPO) & cd ../codist-ui && npm run dev

# One binary per system, ready to copy.
dist:
	GOOS=darwin  GOARCH=arm64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-x64 .
	GOOS=linux   GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-linux-x64 .
	GOOS=linux   GOARCH=arm64 $(GO) build -tags '$(TAGS)' -o dist/codist-linux-arm64 .
	GOOS=windows GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-windows-x64.exe .

clean:
	rm -rf bin dist
