# Codist builds as one file with no C compiler. The interface (web/) is built with Node and embedded;
# only the Java grammar is embedded in the parser, which keeps the binary small.
TAGS := grammar_subset grammar_subset_java
GO   := CGO_ENABLED=0 go

.PHONY: build ui test run dev dist clean

build: ui
	$(GO) build -tags '$(TAGS)' -o bin/codist .

ui:
	cd web && npm install --no-audit --no-fund && npm run build
	rm -rf internal/web/dist/assets internal/web/dist/index.html internal/web/dist/favicon.svg
	cp -R web/dist/. internal/web/dist/

test:
	$(GO) test -tags '$(TAGS)' ./...

run: build
	./bin/codist $(REPO)

# Go server on :8080 and the interface with live reload on :5173.
dev:
	@trap 'kill 0' EXIT; $(GO) run -tags '$(TAGS)' . --no-open $(REPO) & cd web && npm run dev

# One binary per system, ready to copy.
dist: ui
	GOOS=darwin  GOARCH=arm64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-arm64 .
	GOOS=darwin  GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-macos-x64 .
	GOOS=linux   GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-linux-x64 .
	GOOS=windows GOARCH=amd64 $(GO) build -tags '$(TAGS)' -o dist/codist-windows-x64.exe .

clean:
	rm -rf bin dist web/dist internal/web/dist/assets internal/web/dist/index.html
