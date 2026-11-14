.PHONY: all build test test-race lint frontend-install frontend-build frontend-check frontend-test licenses clean

PYTHON ?= python

all: build test-race lint

web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	npm ci --prefix web

frontend-install:
	npm ci --prefix web

frontend-build: web/node_modules/.package-lock.json
	npm run build --prefix web

frontend-check: web/node_modules/.package-lock.json
	npm run check --prefix web

frontend-test: web/node_modules/.package-lock.json
	npm test --prefix web

licenses: web/node_modules/.package-lock.json
	$(PYTHON) packaging/licenses.py --check

build: frontend-build licenses
	go build ./cmd/anilist-arr-sync

test: frontend-build licenses frontend-test
	go test ./...

test-race: frontend-build licenses frontend-test
	go test -race ./...

lint: frontend-build frontend-check licenses
	go vet ./...
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

clean:
	go clean ./cmd/anilist-arr-sync
	$(PYTHON) -c "from pathlib import Path; import shutil; output = Path('web/dist'); assert output.resolve() == Path.cwd().resolve() / 'web/dist'; [shutil.rmtree(p) if p.is_dir() else p.unlink() for p in output.glob('*') if p.name != '.gitkeep']"
