# Codist: Go server (server/) + Svelte UI (web/).
#   make run REPO=~/dev/demo-shop   build everything and serve on http://localhost:8080
#   make dev REPO=~/dev/demo-shop   Go server + Vite dev server with hot reload (http://localhost:5173)

REPO ?= $(HOME)/dev/demo-shop
export CODIST_REPO ?= $(REPO)

.PHONY: install build run dev test clean

install:
	cd web && npm install
	cd server && go mod download

build:
	cd web && npm run build
	cd server && go build -o bin/codist .

run: build
	cd server && ./bin/codist

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
