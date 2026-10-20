.PHONY: all build test test-race lint frontend-install frontend-build frontend-test clean

all: build

frontend-install:
	cd web && npm ci

frontend-build:
	cd web && npm run build

frontend-test:
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
