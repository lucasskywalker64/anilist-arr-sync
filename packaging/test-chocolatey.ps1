param([Parameter(Mandatory)][ValidateSet('amd64', 'arm64')][string]$ExpectedArchitecture)
$ErrorActionPreference = 'Stop'
$native = (Get-ItemProperty 'HKLM:\SYSTEM\CurrentControlSet\Control\Session Manager\Environment' -Name PROCESSOR_ARCHITECTURE).PROCESSOR_ARCHITECTURE
if ($native -ne @{ amd64 = 'AMD64'; arm64 = 'ARM64' }[$ExpectedArchitecture]) { throw 'Fixture requires the matching native Windows runner' }
$installScript = (Resolve-Path .release/chocolatey/tools/chocolateyinstall.ps1).Path
$chocolatey = $env:ChocolateyInstall
$fixture = Join-Path (Resolve-Path .release-tools).Path ('choco-' + [Guid]::NewGuid().ToString('N').Substring(0, 16))
$previousInstall = $env:ChocolateyInstall
New-Item -ItemType Directory -Path "$fixture/distribution/lib", "$fixture/package/tools", "$fixture/source" | Out-Null
try {
    # Isolate Chocolatey's package database, configuration and logs. Never install
    # the application: the fixture intercepts only its final installer handoff.
    Copy-Item -LiteralPath (Join-Path $chocolatey 'choco.exe') -Destination "$fixture/distribution"
    foreach ($directory in @('helpers', 'redirects', 'tools')) {
        Copy-Item -LiteralPath (Join-Path $chocolatey $directory) -Destination "$fixture/distribution" -Recurse
    }
    Copy-Item -LiteralPath $installScript -Destination "$fixture/package/tools/generated-install.ps1"
    Set-Content -LiteralPath "$fixture/package/architecture-fixture.nuspec" -Value @'
<?xml version="1.0"?>
<package><metadata><id>anilist-arch-fixture</id><version>0.0.1</version><authors>Fixture</authors><description>Local architecture selection fixture</description></metadata><files><file src="tools/**" target="tools" /></files></package>
'@
    Set-Content -LiteralPath "$fixture/package/tools/chocolateyinstall.ps1" -Value @'
$ErrorActionPreference = 'Stop'
function Install-ChocolateyPackage {
    param($PackageName, $FileType, $SilentArgs, $Url, $Checksum, $ChecksumType, $ValidExitCodes)
    if ($Url -notmatch ('_windows_' + $env:ANILIST_FIXTURE_ARCH + '_setup\.exe$')) { throw "Wrong native installer: $Url" }
    if ($Checksum -notmatch '^[0-9a-f]{64}$' -or $ChecksumType -ne 'sha256') { throw 'Installer checksum handoff changed' }
    if (!$SilentArgs.Contains('/UPDATEOWNER=chocolatey') -or !$SilentArgs.Contains('/TYPE=service')) { throw 'Installer ownership handoff changed' }
    Set-Content -LiteralPath (Join-Path $PSScriptRoot 'selection-passed') -Value $env:ANILIST_FIXTURE_ARCH
}
. (Join-Path $PSScriptRoot 'generated-install.ps1')
'@
    $env:ChocolateyInstall = (Resolve-Path "$fixture/distribution").Path
    $env:ANILIST_FIXTURE_ARCH = $ExpectedArchitecture
    & "$fixture/distribution/choco.exe" pack "$fixture/package/architecture-fixture.nuspec" --basepath "$fixture/package" --outputdirectory "$fixture/source" --limit-output
    if ($LASTEXITCODE -ne 0) { throw 'Cannot pack Chocolatey architecture fixture' }
    $package = [IO.Compression.ZipFile]::OpenRead("$fixture/source/anilist-arch-fixture.0.0.1.nupkg")
    try {
        if (@($package.Entries | Where-Object FullName -eq 'tools/chocolateyinstall.ps1').Count -ne 1) { throw "Missing Chocolatey entry point: $($package.Entries.FullName -join ', ')" }
    } finally { $package.Dispose() }
    & "$fixture/distribution/choco.exe" install anilist-arch-fixture --version 0.0.1 --source "$fixture/source" --yes --no-progress --limit-output --skip-autouninstaller --execution-timeout 90
    if ($LASTEXITCODE -ne 0) { throw 'Chocolatey architecture fixture failed' }
    $marker = "$fixture/distribution/lib/anilist-arch-fixture/tools/selection-passed"
    if (!(Test-Path -LiteralPath $marker) -or (Get-Content -LiteralPath $marker -Raw).Trim() -ne $ExpectedArchitecture) { throw 'Chocolatey did not execute the generated installer script' }
    Write-Host "Chocolatey selected the native $ExpectedArchitecture installer; no application was installed"
} finally {
    $env:ChocolateyInstall = $previousInstall
    Remove-Item Env:ANILIST_FIXTURE_ARCH -ErrorAction SilentlyContinue
    $resolved = [IO.Path]::GetFullPath($fixture)
    $allowed = (Resolve-Path .release-tools).Path + [IO.Path]::DirectorySeparatorChar
    if (!$resolved.StartsWith($allowed, [StringComparison]::OrdinalIgnoreCase)) { throw 'Fixture cleanup escaped .release-tools' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
