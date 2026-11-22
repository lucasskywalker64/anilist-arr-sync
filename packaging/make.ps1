param(
    [Parameter(Mandatory)][ValidateSet('windows-resources', 'installer', 'installer-process-test', 'installer-test', 'chocolatey-test', 'release-snapshot')][string]$Task,
    [string]$Arch = 'amd64',
    [string]$ReleaseVersion = 'v0.0.0-local',
    [string]$ReleaseCommit = '',
    [string]$InstallerPath = ''
)
$ErrorActionPreference = 'Stop'

function Add-LocalToolsToPath([string[]]$Dirs) {
    foreach ($dir in $Dirs) {
        if (Test-Path -LiteralPath $dir) {
            $env:PATH = (Resolve-Path -LiteralPath $dir).Path + ';' + $env:PATH
        }
    }
}

switch ($Task) {
    'windows-resources' {
        New-Item -ItemType Directory -Path .release -Force | Out-Null
        $env:RELEASE_VERSION = $ReleaseVersion
        & "$PSScriptRoot/resources.ps1"
    }
    'installer' {
        New-Item -ItemType Directory -Path .release -Force | Out-Null
        Add-LocalToolsToPath @('.release-tools/inno')
        $stage = Join-Path ([IO.Path]::GetTempPath()) ('anilist-installer-bin-' + [Guid]::NewGuid().ToString('N'))
        New-Item -ItemType Directory -Path $stage -Force | Out-Null
        try {
            $env:GOOS = 'windows'
            $env:GOARCH = $Arch
            $env:CGO_ENABLED = '0'
            $bin = Join-Path $stage 'anilist-arr-sync.exe'
            $ldflags = "-s -w -H windowsgui -X main.version=$ReleaseVersion -X main.commit=$ReleaseCommit"
            go build "-ldflags=$ldflags" -o $bin ./cmd/anilist-arr-sync
            if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
            $env:RELEASE_VERSION = $ReleaseVersion
            $env:RELEASE_COMMIT = $ReleaseCommit
            $env:RELEASE_MODE = 'fixture'
            $env:RELEASE_INSTALL_TEST = '0'
            & "$PSScriptRoot/windows.ps1" -Binary $bin -TargetOS windows -Arch $Arch
        } finally {
            if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
        }
    }
    'installer-process-test' {
        Add-LocalToolsToPath @('.release-tools/inno')
        & "$PSScriptRoot/test-installer-process.ps1"
    }
    'installer-test' {
        $env:RELEASE_VERSION = $ReleaseVersion
        $env:RELEASE_COMMIT = $ReleaseCommit
        $env:RELEASE_MODE = 'fixture'
        & "$PSScriptRoot/test-installer.ps1" -Installer $InstallerPath -Arch $Arch
    }
    'chocolatey-test' {
        & "$PSScriptRoot/test-chocolatey.ps1" -ExpectedArchitecture $Arch
    }
    'release-snapshot' {
        Add-LocalToolsToPath @('.release-tools/inno', '.release-tools/goreleaser', '.release-tools/bin')
        $env:RELEASE_MODE = 'fixture'
        $env:RELEASE_SNAPSHOT_TEST = '1'
        $env:RELEASE_INSTALL_TEST = '1'
        go test -v ./cmd/release -run '^TestSnapshot$' -count=1 -timeout=30m
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
}
