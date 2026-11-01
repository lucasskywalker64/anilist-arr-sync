.PHONY: all build test test-race lint frontend-install frontend-build frontend-test clean

all: build

web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	cd web && npm ci
	@touch web/node_modules/.package-lock.json

frontend-install: web/node_modules/.package-lock.json

frontend-build: web/node_modules/.package-lock.json
	cd web && npm run build

frontend-test: web/node_modules/.package-lock.json
	cd web && npm test

build: frontend-build
	go build -v ./...

test: frontend-build
	go test -v ./...

test-race: frontend-build
	go test -v -race ./...

lint: frontend-build
	go vet ./...
	golangci-lint run
