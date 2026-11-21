.PHONY: all verify-elevated help \
	frontend-install frontend-dev frontend-build frontend-preview frontend-check frontend-test \
	licenses licenses-update \
	fmt-check go-vet go-lint go-test go-test-race build devserver test test-race lint \
	packaging-release-test packaging-production-test packaging-test \
	windows-resources installer installer-process-test installer-test chocolatey-test release-snapshot \
	docker clean

PYTHON ?= python
PWSH ?= pwsh
NODE ?= node
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
ARCH ?= $(GOARCH)
RELEASE_VERSION ?= v0.0.0-local
RELEASE_COMMIT ?= $(shell git rev-parse HEAD)
DOCKER_IMAGE ?= anilist-arr-sync:local
INSTALLER_PATH ?= .release/anilist-arr-sync_$(RELEASE_VERSION)_windows_$(ARCH)_setup.exe

ifeq ($(GOOS),windows)
APP_LDFLAGS = -ldflags="-H windowsgui"
ALL_PLATFORM_TARGETS = installer installer-process-test
else
ALL_PLATFORM_TARGETS =
endif

all: build test-race lint packaging-test $(ALL_PLATFORM_TARGETS)

verify-elevated: all installer-test

help:
	@$(PYTHON) -c "print('\n'.join([ \
		'Aggregate targets:', \
		'  all                       Build app, run tests/race/lint/packaging checks (plus installer + process test on Windows)', \
		'  verify-elevated           Run make all plus elevated Windows installer lifecycle tests (Admin shell)', \
		'Frontend targets:', \
		'  frontend-install          Install locked frontend dependencies (npm ci)', \
		'  frontend-dev              Start Vite frontend dev server', \
		'  frontend-build            Build embedded frontend into web/dist', \
		'  frontend-preview          Preview built frontend', \
		'  frontend-check            Run svelte-check / TypeScript typechecking', \
		'  frontend-test             Run Vitest frontend tests', \
		'Go and notice targets:', \
		'  licenses                  Verify THIRD_PARTY_NOTICES.txt matches audited dependencies', \
		'  licenses-update           Regenerate THIRD_PARTY_NOTICES.txt', \
		'  fmt-check                 Check gofmt formatting across all Go files', \
		'  go-vet                    Run go vet ./...', \
		'  go-lint                   Run golangci-lint v2.14.0', \
		'  go-test                   Run go test ./...', \
		'  go-test-race              Run go test -race ./...', \
		'  build                     Build frontend, check notices, and compile anilist-arr-sync', \
		'  devserver                 Run the Go devserver helper', \
		'  test                      Build frontend, check notices, and run frontend + Go tests', \
		'  test-race                 Build frontend, check notices, and run frontend + Go race tests', \
		'  lint                      Build frontend, check types/notices, and run fmt-check + go-vet + go-lint', \
		'Packaging and release targets:', \
		'  packaging-release-test    Run packaging/release_test.py', \
		'  packaging-production-test Run packaging/production.test.cjs', \
		'  packaging-test            Run all cross-platform packaging tests', \
		'  windows-resources         Generate Windows versioninfo .syso resources', \
		'  installer                 Build Windows GUI binary and Inno Setup installer into .release/', \
		'  installer-process-test    Run non-admin packaging/test-installer-process.ps1', \
		'  installer-test            Run elevated packaging/test-installer.ps1 against the built installer', \
		'  chocolatey-test           Run packaging/test-chocolatey.ps1 (requires Chocolatey and manifests)', \
		'  release-snapshot          Run full disposable-key GoReleaser snapshot verification (Admin shell)', \
		'  docker                    Build container image ($(DOCKER_IMAGE))', \
		'  clean                     Remove local binary, web/dist build output, and generated .syso files', \
	]))"

web/node_modules/.package-lock.json: web/package.json web/package-lock.json
	npm ci --prefix web

frontend-install:
	npm ci --prefix web

frontend-dev: web/node_modules/.package-lock.json
	npm run dev --prefix web

frontend-build: web/node_modules/.package-lock.json
	npm run build --prefix web

frontend-preview: web/node_modules/.package-lock.json
	npm run preview --prefix web

frontend-check: web/node_modules/.package-lock.json
	npm run check --prefix web

frontend-test: web/node_modules/.package-lock.json
	npm test --prefix web

licenses: web/node_modules/.package-lock.json
	$(PYTHON) packaging/licenses.py --check

licenses-update: web/node_modules/.package-lock.json
	$(PYTHON) packaging/licenses.py

fmt-check:
	$(PYTHON) -c "import subprocess, sys; out = subprocess.check_output(['gofmt', '-l', '.'], text=True).strip(); sys.exit('Unformatted Go files:\n' + out) if out else None"

go-vet: frontend-build
	go vet ./...

go-lint: frontend-build
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

go-test: frontend-build
	go test ./...

go-test-race: frontend-build
	go test -race ./...

build: frontend-build licenses
	go build $(APP_LDFLAGS) ./cmd/anilist-arr-sync

devserver:
	go run ./cmd/devserver

test: frontend-build licenses frontend-test go-test

test-race: frontend-build licenses frontend-test go-test-race

lint: frontend-build frontend-check licenses fmt-check go-vet go-lint

packaging-release-test:
	$(PYTHON) -m unittest discover -s packaging -p "*_test.py"

packaging-production-test:
	$(NODE) --test packaging/production.test.cjs

packaging-test: packaging-release-test packaging-production-test

windows-resources:
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; New-Item -ItemType Directory -Path .release -Force | Out-Null; $$env:RELEASE_VERSION = '$(RELEASE_VERSION)'; & ./packaging/resources.ps1"

installer: frontend-build licenses windows-resources
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; New-Item -ItemType Directory -Path .release -Force | Out-Null; if (Test-Path .release-tools/inno) { $$env:PATH = (Resolve-Path .release-tools/inno).Path + ';' + $$env:PATH }; $$env:GOOS = 'windows'; $$env:GOARCH = '$(ARCH)'; $$env:CGO_ENABLED = '0'; $$bin = '.release/anilist-arr-sync_$(ARCH).exe'; go build -ldflags='-s -w -H windowsgui -X main.version=$(RELEASE_VERSION) -X main.commit=$(RELEASE_COMMIT)' -o $$bin ./cmd/anilist-arr-sync; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE }; $$env:RELEASE_VERSION = '$(RELEASE_VERSION)'; $$env:RELEASE_COMMIT = '$(RELEASE_COMMIT)'; $$env:RELEASE_MODE = 'fixture'; $$env:RELEASE_INSTALL_TEST = '0'; & ./packaging/windows.ps1 -Binary $$bin -TargetOS windows -Arch '$(ARCH)'"

installer-process-test:
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; if (Test-Path .release-tools/inno) { $$env:PATH = (Resolve-Path .release-tools/inno).Path + ';' + $$env:PATH }; & ./packaging/test-installer-process.ps1"

installer-test: installer
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; $$env:RELEASE_VERSION = '$(RELEASE_VERSION)'; $$env:RELEASE_COMMIT = '$(RELEASE_COMMIT)'; $$env:RELEASE_MODE = 'fixture'; & ./packaging/test-installer.ps1 -Installer '$(INSTALLER_PATH)' -Arch '$(ARCH)'"

chocolatey-test:
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; & ./packaging/test-chocolatey.ps1 -ExpectedArchitecture '$(ARCH)'"

release-snapshot: frontend-build licenses
	$(PWSH) -NoProfile -Command "$$ErrorActionPreference = 'Stop'; foreach ($$toolDir in @('.release-tools/inno', '.release-tools/goreleaser', '.release-tools/bin')) { if (Test-Path $$toolDir) { $$env:PATH = (Resolve-Path $$toolDir).Path + ';' + $$env:PATH } }; $$env:RELEASE_MODE = 'fixture'; $$env:RELEASE_SNAPSHOT_TEST = '1'; $$env:RELEASE_INSTALL_TEST = '1'; go test -v ./cmd/release -run '^TestSnapshot$$' -count=1 -timeout=30m"

docker:
	docker build -t $(DOCKER_IMAGE) .

clean:
	go clean ./cmd/anilist-arr-sync
	$(PYTHON) -c "from pathlib import Path; import shutil; output = Path('web/dist'); assert output.resolve() == Path.cwd().resolve() / 'web/dist'; [shutil.rmtree(p) if p.is_dir() else p.unlink() for p in output.glob('*') if p.name != '.gitkeep']; [p.unlink() for p in Path('cmd/anilist-arr-sync').glob('resource_windows_*.syso')]"
