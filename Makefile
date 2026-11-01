# Codist: Go server (server/) + Svelte UI (web/).
#   make run REPO=~/dev/demo-shop   build everything and serve on http://localhost:8080
#   make dev REPO=~/dev/demo-shop   Go server + Vite dev server with hot reload (http://localhost:5173)

REPO ?= $(HOME)/dev/demo-shop
export CODIST_REPO ?= $(REPO)

.PHONY: install build run dev test clean dist install-bin

install:
	cd web && npm install
	cd server && go mod download

build:
	cd web && npm run build
	rm -rf server/internal/webui/dist/assets server/internal/webui/dist/index.html
	cp -R web/dist/. server/internal/webui/dist/
	cd server && go build -o bin/codist .

run: build
	cd server && ./bin/codist --no-open

# One file with the UI inside it, for this machine: copy it anywhere and run it.
dist: build
	mkdir -p dist
	cp server/bin/codist dist/codist-$$(go env GOOS)-$$(go env GOARCH)
	@ls -lh dist/

# Puts codist on your PATH.
install-bin: build
	mkdir -p $(HOME)/.local/bin
	cp server/bin/codist $(HOME)/.local/bin/codist
	@echo "Installed. If codist is not found, add $(HOME)/.local/bin to your PATH."

dev:
	@trap 'kill 0' EXIT; \
	(cd server && go run .) & \
	(cd web && npm run dev) & \
	wait

test:
	cd server && go test ./...
	cd web && npm run check

clean:
	rm -rf server/bin web/dist
