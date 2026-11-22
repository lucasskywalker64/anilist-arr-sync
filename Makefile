.PHONY: all verify-elevated help \
	frontend-install frontend-dev frontend-build frontend-preview frontend-check frontend-test \
	licenses licenses-update \
	fmt-check go-vet go-lint go-test go-test-race build test test-race lint \
	packaging-release-test packaging-production-test packaging-test \
	windows-resources installer installer-only installer-process-test installer-test chocolatey-test release-snapshot \
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
all:
	$(MAKE) windows-resources
	$(MAKE) build test-race lint packaging-test installer-only installer-process-test
else
all: build test-race lint packaging-test
endif

verify-elevated: all
	$(MAKE) installer-test

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
		'  docker                    Stage Linux binary and build container image ($(DOCKER_IMAGE))', \
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

test: frontend-build licenses frontend-test go-test

test-race: frontend-build licenses frontend-test go-test-race

lint: frontend-build frontend-check licenses fmt-check go-vet go-lint

packaging-release-test:
	$(PYTHON) -m unittest discover -s packaging -p "*_test.py"

packaging-production-test:
	$(NODE) --test packaging/production.test.cjs

packaging-test: packaging-release-test packaging-production-test

windows-resources:
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task windows-resources -ReleaseVersion "$(RELEASE_VERSION)"

installer-only: frontend-build licenses
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task installer -Arch "$(ARCH)" -ReleaseVersion "$(RELEASE_VERSION)" -ReleaseCommit "$(RELEASE_COMMIT)"

installer: frontend-build licenses windows-resources
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task installer -Arch "$(ARCH)" -ReleaseVersion "$(RELEASE_VERSION)" -ReleaseCommit "$(RELEASE_COMMIT)"

installer-process-test:
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task installer-process-test

installer-test: installer
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task installer-test -Arch "$(ARCH)" -ReleaseVersion "$(RELEASE_VERSION)" -ReleaseCommit "$(RELEASE_COMMIT)" -InstallerPath "$(INSTALLER_PATH)"

chocolatey-test:
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task chocolatey-test -Arch "$(ARCH)"

release-snapshot: frontend-build licenses
	$(PWSH) -NoProfile -File packaging/make.ps1 -Task release-snapshot

docker: frontend-build licenses
	$(PYTHON) -c "import atexit, os, shutil, subprocess, sys, tempfile; from pathlib import Path; arch, ver, commit, image = sys.argv[1:5]; tmp = tempfile.TemporaryDirectory(prefix='anilist-docker-'); atexit.register(tmp.cleanup); root = Path(tmp.name); binary = root / 'linux' / arch / 'anilist-arr-sync'; binary.parent.mkdir(parents=True, exist_ok=True); env = {**os.environ, 'GOOS': 'linux', 'GOARCH': arch, 'CGO_ENABLED': '0'}; ldflags = f'-s -w -X main.version={ver} -X main.commit={commit}'; subprocess.run(['go', 'build', f'-ldflags={ldflags}', '-o', str(binary), './cmd/anilist-arr-sync'], check=True, env=env); shutil.copy2('Dockerfile', root / 'Dockerfile'); shutil.copytree('packaging/container-config', root / 'packaging/container-config'); subprocess.run(['docker', 'build', '--platform', f'linux/{arch}', '-t', image, str(root)], check=True)" "$(ARCH)" "$(RELEASE_VERSION)" "$(RELEASE_COMMIT)" "$(DOCKER_IMAGE)"

clean:
	go clean ./cmd/anilist-arr-sync
	$(PYTHON) -c "from pathlib import Path; import shutil; output = Path('web/dist'); assert output.resolve() == Path.cwd().resolve() / 'web/dist'; [shutil.rmtree(p) if p.is_dir() else p.unlink() for p in output.glob('*') if p.name != '.gitkeep']; [p.unlink() for p in Path('cmd/anilist-arr-sync').glob('resource_windows_*.syso')]"
