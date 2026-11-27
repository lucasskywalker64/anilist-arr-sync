param([Parameter(Mandatory)][string]$Binary, [string]$TargetOS, [ValidateSet('amd64', 'arm64')][string]$Arch)
$ErrorActionPreference = 'Stop'
if ($TargetOS -ne 'windows') { exit 0 }
$signWindows = $false
if ($env:RELEASE_MODE -eq 'production') {
    if ($env:PROCESSOR_ARCHITECTURE -ne 'ARM64') {
        throw 'Production requires the Windows ARM64 runner to execute both installer architectures'
    }
    if ($env:RELEASE_SIGN_WINDOWS -ceq 'true') {
        $signWindows = $true
    } elseif ($env:RELEASE_SIGN_WINDOWS -cne 'false') {
        throw "RELEASE_SIGN_WINDOWS must be 'true' or 'false' in production mode"
    }
}
$Binary = (Resolve-Path -LiteralPath $Binary).Path
if ($signWindows) {
    node packaging/signpath.mjs $Binary
    if ($LASTEXITCODE -ne 0) { throw 'Application signing failed' }
    & "$PSScriptRoot/verify-authenticode.ps1" -Path $Binary
}
$iscc = (Get-Command ISCC.exe -ErrorAction SilentlyContinue).Source
if (!$iscc) { $iscc = "${env:ProgramFiles}\Inno Setup 7\ISCC.exe" }
if (!(Test-Path -LiteralPath $iscc)) { throw 'Inno Setup 7.1.1 is required' }
$output = (Resolve-Path .release).Path
$args = @('/Qp', "/DBinary=$Binary", "/DArch=$Arch", "/DReleaseVersion=$env:RELEASE_VERSION", "/DOutputDir=$output")
if ($signWindows) {
    # Inno invokes this for the uninstaller, temporary setup copies, and setup.
    # SignPath's trusted GitHub connector requires a workflow artifact each time.
    $args += '/DProduction=1'
    $command = 'node $q' + (Join-Path $PSScriptRoot 'signpath.mjs') + '$q $f'
    $args += "/Ssignpath=$command"
}
& $iscc @args "$PSScriptRoot/windows.iss"
if ($LASTEXITCODE -ne 0) { throw "Inno Setup failed: $Arch" }
$installer = Join-Path $output "anilist-arr-sync_$($env:RELEASE_VERSION)_windows_$($Arch)_setup.exe"
if ($signWindows) { & "$PSScriptRoot/verify-authenticode.ps1" -Path $installer }
if ($env:RELEASE_INSTALL_TEST -eq '1') {
    $nativeArch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
    # The PR matrix covers native installers. Publication's single ARM host
    # verifies both installers before publishing.
    if ($Arch -eq $nativeArch -or ($env:RELEASE_MODE -ne 'fixture' -and $nativeArch -eq 'arm64')) {
        & "$PSScriptRoot/test-installer.ps1" -Installer $installer -Arch $Arch
        if (!$?) { throw 'Installer lifecycle verification failed' }
        $hash = (Get-FileHash -LiteralPath $installer -Algorithm SHA256).Hash.ToLowerInvariant()
        "$env:RELEASE_COMMIT $hash" | Set-Content ".release/installer-verified-$Arch" -Encoding ascii
    }
}
