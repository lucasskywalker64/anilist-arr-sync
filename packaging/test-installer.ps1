param([Parameter(Mandatory)][string]$Installer, [ValidateSet('amd64', 'arm64')][string]$Arch)
$ErrorActionPreference = 'Stop'
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Installer lifecycle tests require an administrator token' }
# Refuse to upgrade an existing real installation or overwrite its shortcuts.
$key = "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\AniList-Arr-Sync-${Arch}_is1"
if (Test-Path -LiteralPath $key) { throw 'Existing installation blocks isolated installer verification' }
$startup = Join-Path ([Environment]::GetFolderPath('Startup')) 'AniList-Arr-Sync.lnk'
if (Test-Path -LiteralPath $startup) { throw 'Existing startup shortcut blocks isolated installer verification' }
$releaseRoot = (Resolve-Path .release).Path
$root = Join-Path $releaseRoot ('install-test-' + [Guid]::NewGuid().ToString('N'))
$app = Join-Path $root 'app'
$data = Join-Path $root 'data'
$service = 'AniListArrSyncFixture' + [Guid]::NewGuid().ToString('N')
$group = 'AniList-Arr-Sync fixture ' + $service
$tray = $null

function Install([string]$Owner, [string]$Type = 'service') {
    $arguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/TYPE=$Type", "/SERVICENAME=$service", "/DIR=`"$app`"", "/DATADIR=`"$data`"", "/GROUP=`"$group`"")
    if ($Owner) { $arguments += "/UPDATEOWNER=$Owner" }
    $process = Start-Process -FilePath $Installer -ArgumentList $arguments -PassThru -Wait -WindowStyle Hidden
    if ($process.ExitCode -ne 0) { throw "Installer failed with exit code $($process.ExitCode)" }
}

function WaitHealthy {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    while ([DateTime]::UtcNow -lt $deadline) {
        try {
            $health = Invoke-RestMethod http://127.0.0.1:7171/api/v1/health -TimeoutSec 1
            if ($health.version -eq $env:RELEASE_VERSION -and $health.commit -eq $env:RELEASE_COMMIT -and $health.instanceId) { return }
        } catch { }
        Start-Sleep -Milliseconds 100
    }
    throw 'Installed application failed exact version/commit health check'
}

try {
    # A foreign listener must not satisfy the fixture's health checks.
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 7171)
    $listener.Start()
    $listener.Stop()
    Install ''
    WaitHealthy
    if (Test-Path -LiteralPath (Join-Path $app 'update-owner')) { throw 'Direct installer created an ownership marker' }
    $registered = Get-CimInstance Win32_Service -Filter "Name='$service'"
    if ($registered.ServiceType -ne 'Own Process' -or !$registered.PathName.Contains((Join-Path $app 'anilist-arr-sync.exe')) -or !$registered.PathName.Contains($data) -or !$registered.PathName.Contains('--config')) {
        throw 'Service registration lost its type, executable, or persistent absolute paths'
    }
    foreach ($owner in 'winget', 'chocolatey', '') {
        Install $owner
        WaitHealthy
        $expectedOwner = if ($owner) { $owner } else { 'chocolatey' }
        if ((Get-Content -LiteralPath (Join-Path $app 'update-owner') -Raw).Trim() -ne $expectedOwner) { throw 'Ownership was not preserved/recreated during upgrade' }
    }
    if ($env:RELEASE_MODE -eq 'production') {
        & "$PSScriptRoot/verify-authenticode.ps1" -Path (Join-Path $app 'anilist-arr-sync.exe')
        & "$PSScriptRoot/verify-authenticode.ps1" -Path (Join-Path $app 'unins000.exe')
    }
    Install '' 'desktop'
    if (Get-Service -Name $service -ErrorAction SilentlyContinue) { throw 'Desktop mode retained the Windows service' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($startup)
    if ($shortcut.TargetPath -ne (Join-Path $app 'anilist-arr-sync.exe') -or !$shortcut.Arguments.Contains('--tray') -or !$shortcut.Arguments.Contains($data)) { throw 'Desktop startup shortcut lost tray/data arguments' }
    $tray = Start-Process -FilePath (Join-Path $app 'anilist-arr-sync.exe') -ArgumentList @('--tray', '--data-dir', "`"$data`"", '--config', "`"$data\config.xml`"") -PassThru -WindowStyle Hidden
    WaitHealthy
    Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class FixtureTray {
    public delegate bool Callback(IntPtr window, IntPtr arg);
    [DllImport("user32.dll")] public static extern bool EnumWindows(Callback callback, IntPtr arg);
    [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr window, out uint process);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr window, uint message, IntPtr wparam, IntPtr lparam);
}
'@
    $trayId = $tray.Id
    [FixtureTray]::EnumWindows({ param($window, $arg)
        [uint32]$windowProcess = 0
        [void][FixtureTray]::GetWindowThreadProcessId($window, [ref]$windowProcess)
        if ($windowProcess -eq $trayId) { [void][FixtureTray]::PostMessage($window, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero) }
        return $true
    }, [IntPtr]::Zero) | Out-Null
    if (!$tray.WaitForExit(30000)) { throw 'Tray window did not drain the runtime on close' }
    if ($tray.ExitCode -ne 0) { throw 'Tray exited with an error' }
    # Uninstall must also close an active tray itself, before deleting files.
    $tray = Start-Process -FilePath (Join-Path $app 'anilist-arr-sync.exe') -ArgumentList @('--tray', '--data-dir', "`"$data`"", '--config', "`"$data\config.xml`"") -PassThru -WindowStyle Hidden
    WaitHealthy
    $uninstaller = Join-Path $app 'unins000.exe'
    $process = Start-Process -FilePath $uninstaller -ArgumentList '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -PassThru -Wait -WindowStyle Hidden
    if ($process.ExitCode -ne 0) { throw 'Uninstaller failed' }
    if (!$tray.WaitForExit(30000)) { throw 'Uninstall left the tray runtime running' }
    if (Test-Path -LiteralPath (Join-Path $app 'update-owner')) { throw 'Uninstall retained ownership marker' }
    if (Test-Path -LiteralPath (Join-Path $app 'anilist-arr-sync.exe')) { throw 'Uninstall retained executable' }
    if (Test-Path -LiteralPath $startup) { throw 'Uninstall retained startup shortcut' }
    if (!(Test-Path -LiteralPath (Join-Path $data 'config.xml'))) { throw 'Uninstall deleted persistent user data' }
} finally {
    if ($tray -and !$tray.HasExited) { Stop-Process -Id $tray.Id -Force }
    $remaining = Get-Service -Name $service -ErrorAction SilentlyContinue
    if ($remaining) {
        Stop-Service -Name $service -Force -ErrorAction SilentlyContinue
        & sc.exe delete $service | Out-Null
    }
    if (Test-Path -LiteralPath (Join-Path $app 'unins000.exe')) {
        Start-Process -FilePath (Join-Path $app 'unins000.exe') -ArgumentList '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -Wait -WindowStyle Hidden
    }
    # Delete only the uniquely created fixture tree beneath this workspace.
    $resolved = [IO.Path]::GetFullPath($root)
    if ([IO.Path]::GetDirectoryName($resolved) -ne $releaseRoot -or [IO.Path]::GetFileName($resolved) -notlike 'install-test-*') { throw 'Unsafe fixture cleanup target' }
    if (Test-Path -LiteralPath $resolved) { Remove-Item -LiteralPath $resolved -Recurse -Force }
}
