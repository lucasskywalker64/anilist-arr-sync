param([Parameter(Mandatory)][string]$Installer, [ValidateSet('amd64', 'arm64')][string]$Arch)
$ErrorActionPreference = 'Stop'
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$principal = [Security.Principal.WindowsPrincipal]::new([Security.Principal.WindowsIdentity]::GetCurrent())
if (!$principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Installer lifecycle tests require an administrator token' }
# Refuse to upgrade an existing real installation or overwrite its shortcuts.
foreach ($identity in @('AniList-Arr-Sync', 'AniList-Arr-Sync-amd64', 'AniList-Arr-Sync-arm64')) {
    $key = "HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\${identity}_is1"
    if (Test-Path -LiteralPath $key) { throw 'Existing installation blocks isolated installer verification' }
}
$startup = Join-Path ([Environment]::GetFolderPath('Startup')) 'AniList-Arr-Sync.lnk'
$desktop = Join-Path ([Environment]::GetFolderPath('CommonDesktopDirectory')) 'AniList-Arr-Sync'
if (Test-Path -LiteralPath @($startup, "$desktop.lnk", "$desktop.url") | Where-Object { $_ }) {
    throw 'Existing shortcuts block isolated installer verification'
}
$defaultData = Join-Path ([Environment]::GetFolderPath('CommonApplicationData')) 'AniList-Arr-Sync'
if (Test-Path -LiteralPath $defaultData) {
    throw 'Existing default application data blocks isolated installer verification'
}
$createdDefaultData = $false
$lastInstallerLog = $null
$releaseRoot = (Resolve-Path .release).Path
$root = Join-Path $releaseRoot ('install-test-' + [Guid]::NewGuid().ToString('N'))
$app = Join-Path $root 'app'
$data = Join-Path $root 'data'
$service = 'AniListArrSyncFixture' + [Guid]::NewGuid().ToString('N')
$group = 'AniList-Arr-Sync fixture ' + $service
$tray = $null
$trayOutput = $null
$trayError = $null
$stage = 'setup'

Add-Type @'
using System;
using System.Runtime.InteropServices;
public static class FixtureTray {
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string className, string title);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern IntPtr FindWindowEx(IntPtr parent, IntPtr after, string className, string title);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr window, out uint process);
    [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr window, uint message, IntPtr wparam, IntPtr lparam);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern IntPtr CreateWindowEx(uint style, string className, string title, uint windowStyle, int x, int y, int width, int height, IntPtr parent, IntPtr menu, IntPtr instance, IntPtr parameter);
    [DllImport("user32.dll")] static extern bool DestroyWindow(IntPtr window);
    [DllImport("user32.dll", EntryPoint="LoadIconW")] static extern IntPtr LoadIcon(IntPtr instance, IntPtr name);
    [DllImport("shell32.dll", CharSet=CharSet.Unicode)] static extern bool Shell_NotifyIcon(uint message, ref NotifyIcon icon);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern uint RegisterWindowMessage(string message);
    [DllImport("shell32.dll")] static extern int Shell_NotifyIconGetRect(ref IconIdentifier icon, out Rect rect);
    [StructLayout(LayoutKind.Sequential)] struct IconIdentifier {
        public uint Size; public IntPtr Window; public uint Id; public Guid Guid;
    }
    [StructLayout(LayoutKind.Sequential)] struct Rect { public int Left, Top, Right, Bottom; }
    [StructLayout(LayoutKind.Sequential, CharSet=CharSet.Unicode)] struct NotifyIcon {
        public uint Size; public IntPtr Window; public uint Id, Flags, Message; public IntPtr Icon;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=128)] public string Tip;
        public uint State, StateMask;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=256)] public string Info;
        public uint Version;
        [MarshalAs(UnmanagedType.ByValTStr, SizeConst=64)] public string Title;
        public uint InfoFlags; public Guid Guid; public IntPtr Balloon;
    }
    public static bool NotificationAreaReady() {
        IntPtr window = CreateWindowEx(0, "STATIC", "AniList-Arr-Sync fixture probe", 0, 0, 0, 0, 0, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero, IntPtr.Zero);
        if (window == IntPtr.Zero) throw new InvalidOperationException("Cannot create notification probe window");
        try {
            var icon = new NotifyIcon { Size=(uint)Marshal.SizeOf<NotifyIcon>(), Window=window, Id=1, Flags=2|4, Icon=LoadIcon(IntPtr.Zero, new IntPtr(32512)), Tip="AniList-Arr-Sync fixture probe" };
            if (!Shell_NotifyIcon(0, ref icon)) return false;
            Shell_NotifyIcon(2, ref icon);
            return true;
        } finally { DestroyWindow(window); }
    }
    public static bool IconRegistered(IntPtr window) {
        var icon = new IconIdentifier { Size=(uint)Marshal.SizeOf<IconIdentifier>(), Window=window, Id=1 };
        Rect rect;
        return Shell_NotifyIconGetRect(ref icon, out rect) >= 0;
    }
    public static void RemoveIcon(IntPtr window) {
        var icon = new NotifyIcon { Size=(uint)Marshal.SizeOf<NotifyIcon>(), Window=window, Id=1 };
        if (!Shell_NotifyIcon(2, ref icon)) throw new InvalidOperationException("Cannot remove fixture tray icon");
    }
    public static IntPtr AppWindow(uint process) {
        IntPtr window = IntPtr.Zero;
        while ((window = FindWindowEx(IntPtr.Zero, window, "AniListArrSyncTray", null)) != IntPtr.Zero) {
            uint owner; GetWindowThreadProcessId(window, out owner);
            if (owner == process) return window;
        }
        return IntPtr.Zero;
    }
}
'@

function EnsureTrayDesktop {
    # Hosted runners may start without Explorer. Exercise the real tray in a
    # desktop with its notification area; never turn tray verification off.
    if ([FixtureTray]::FindWindow('Shell_TrayWnd', $null) -eq [IntPtr]::Zero) {
        Start-Process -FilePath "$env:SystemRoot\explorer.exe" -WindowStyle Hidden
    }
    $deadline = [DateTime]::UtcNow.AddSeconds(20)
    while ([DateTime]::UtcNow -lt $deadline) {
        if ([FixtureTray]::NotificationAreaReady()) { return }
        Start-Sleep -Milliseconds 100
    }
    $shell = Get-Process explorer -ErrorAction SilentlyContinue | ForEach-Object { "pid=$($_.Id),session=$($_.SessionId)" }
    throw "[$stage] Notification area rejected a native probe (session $([Diagnostics.Process]::GetCurrentProcess().SessionId), interactive=$([Environment]::UserInteractive), arch=$([Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture), explorer=$($shell -join '; '))"
}

function StartTray {
    $start = [Diagnostics.ProcessStartInfo]::new((Join-Path $app 'anilist-arr-sync.exe'))
    $start.UseShellExecute = $false
    $start.RedirectStandardOutput = $true
    $start.RedirectStandardError = $true
    foreach ($argument in @('--tray', '--no-browser', '--data-dir', $data, '--config', (Join-Path $data 'config.xml'))) { $start.ArgumentList.Add($argument) }
    $script:tray = [Diagnostics.Process]::Start($start)
    $script:trayOutput = $tray.StandardOutput.ReadToEndAsync()
    $script:trayError = $tray.StandardError.ReadToEndAsync()
}

function Install([string]$Owner, [string]$Type = 'service', [string]$Tasks, [string]$DataDir = $data) {
    $arguments = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/SERVICENAME=$service", "/DIR=`"$app`"", "/GROUP=`"$group`"")
    if ($DataDir) { $arguments += "/DATADIR=`"$DataDir`"" }
    if ($Type) { $arguments += "/TYPE=$Type" }
    if ($Tasks) { $arguments += "/TASKS=$Tasks" }
    if ($Owner) { $arguments += "/UPDATEOWNER=$Owner" }
    InvokeInstallerProcess $Installer $arguments
}

function Uninstall([string[]]$ExtraArguments = @()) {
    InvokeInstallerProcess (Join-Path $app 'unins000.exe') (@('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') + $ExtraArguments)
}

function InvokeInstallerProcess([string]$File, [string[]]$Arguments, [int]$TimeoutMilliseconds = 120000) {
    New-Item -ItemType Directory -Path $root -Force | Out-Null
    $log = Join-Path $root ('installer-' + [Guid]::NewGuid().ToString('N') + '.log')
    $script:lastInstallerLog = $log
    Write-Host "[$stage] Starting $([IO.Path]::GetFileName($File))"
    # Inno's launcher can exit while its temporary uninstaller is still running.
    # Keep PowerShell's process-tree wait inside a bounded supervisor process.
    $start = [Diagnostics.ProcessStartInfo]::new((Join-Path $PSHOME 'pwsh.exe'))
    $start.UseShellExecute = $false
    $start.CreateNoWindow = $true
    $start.Environment['ANILIST_FIXTURE_INSTALLER_PATH'] = $File
    $start.Environment['ANILIST_FIXTURE_INSTALLER_ARGS'] = ConvertTo-Json -InputObject @($Arguments + "/LOG=`"$log`"") -Compress
    foreach ($argument in @('-NoProfile', '-NonInteractive', '-Command', '$ErrorActionPreference = ''Stop''; $parameters = ConvertFrom-Json -InputObject $env:ANILIST_FIXTURE_INSTALLER_ARGS; $installer = Start-Process -FilePath $env:ANILIST_FIXTURE_INSTALLER_PATH -ArgumentList $parameters -PassThru -Wait -WindowStyle Hidden; exit $installer.ExitCode')) { $start.ArgumentList.Add($argument) }
    $process = [Diagnostics.Process]::Start($start)
    try {
        if (!$process.WaitForExit($TimeoutMilliseconds)) {
            $process.Kill($true)
            throw "[$stage] Installer process timed out after $TimeoutMilliseconds ms"
        }
        if ($process.ExitCode -ne 0) { throw "[$stage] Installer process failed with exit code $($process.ExitCode)" }
        Write-Host "[$stage] Completed"
    } catch {
        if (Test-Path -LiteralPath $log) { Get-Content -LiteralPath $log | Select-Object -Last 40 | ForEach-Object { Write-Host $_ } }
        throw
    } finally {
        $process.Dispose()
    }
}

function AssertInstalledArchitecture([string]$Expected) {
    $bytes = [IO.File]::ReadAllBytes((Join-Path $app 'anilist-arr-sync.exe'))
    $machine = [BitConverter]::ToUInt16($bytes, [BitConverter]::ToInt32($bytes, 0x3c) + 4)
    $wanted = if ($Expected -eq 'arm64') { 0xaa64 } else { 0x8664 }
    if ($machine -ne $wanted) { throw 'Installer wrote the wrong executable architecture' }
    $subsystem = [BitConverter]::ToUInt16($bytes, [BitConverter]::ToInt32($bytes, 0x3c) + 24 + 68)
    if ($subsystem -ne 2) { throw 'Installed executable can create a console window' }
    $entries = @(Get-ChildItem 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall' | Where-Object PSChildName -Like 'AniList-Arr-Sync*_is1')
    if ($entries.Count -ne 1 -or $entries[0].PSChildName -ne 'AniList-Arr-Sync_is1') { throw 'Installer did not preserve one shared installation identity' }
}

function WaitHealthy {
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    $lastHealth = 'no response'
    while ([DateTime]::UtcNow -lt $deadline) {
        if ($tray -and $tray.HasExited) { throw "[$stage] Tray exited before readiness with code $($tray.ExitCode)" }
        try {
            $health = Invoke-RestMethod http://127.0.0.1:7171/api/v1/health -TimeoutSec 1 -NoProxy
            $lastHealth = "version=$($health.version), commit=$($health.commit), instance=$($health.instanceId)"
            if ($health.version -eq $env:RELEASE_VERSION -and $health.commit -eq $env:RELEASE_COMMIT -and $health.instanceId) { return }
        } catch { $lastHealth = $_.Exception.Message }
        Start-Sleep -Milliseconds 100
    }
    throw "[$stage] Installed application failed exact version/commit health check: $lastHealth"
}

try {
    # A foreign listener must not satisfy the fixture's health checks.
    $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, 7171)
    $listener.Start()
    $listener.Stop()
    $stage = 'initial service install'
    Install ''
    AssertInstalledArchitecture $Arch
    if (Test-Path -LiteralPath (Join-Path $app 'update-owner')) { throw 'Direct installer created an ownership marker' }
    $registered = Get-CimInstance Win32_Service -Filter "Name='$service'"
    if ($registered.ServiceType -ne 'Own Process' -or !$registered.PathName.Contains((Join-Path $app 'anilist-arr-sync.exe')) -or !$registered.PathName.Contains($data) -or !$registered.PathName.Contains('--config')) {
        throw 'Service registration lost its type, executable, or persistent absolute paths'
    }
    if ($registered.State -ne 'Stopped' -or $registered.StartMode -ne 'Auto') {
        throw 'Fresh silent install must leave the service stopped and configured for automatic startup'
    }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$desktop.lnk")
    if ($shortcut.TargetPath -ne (Join-Path $app 'anilist-arr-sync.exe') -or !$shortcut.Arguments.Contains('--open-service-dashboard') -or !$shortcut.Arguments.Contains($service) -or !$shortcut.Arguments.Contains($data)) {
        throw 'Service desktop icon lost the installed launcher, service name, or dashboard config path'
    }
    Start-Service -Name $service
    WaitHealthy
    foreach ($owner in 'winget', 'chocolatey', '') {
        $stage = "service upgrade (owner=$owner)"
        Install $owner
        WaitHealthy
        $expectedOwner = if ($owner) { $owner } else { 'chocolatey' }
        if ((Get-Content -LiteralPath (Join-Path $app 'update-owner') -Raw).Trim() -ne $expectedOwner) { throw 'Ownership was not preserved/recreated during upgrade' }
    }
    if ($env:RELEASE_MODE -eq 'production') {
        & "$PSScriptRoot/verify-authenticode.ps1" -Path (Join-Path $app 'anilist-arr-sync.exe')
        & "$PSScriptRoot/verify-authenticode.ps1" -Path (Join-Path $app 'unins000.exe')
    }
    $stage = 'prepare tray desktop'
    EnsureTrayDesktop
    $stage = 'switch to desktop installation'
    Install '' 'desktop'
    if (Get-Service -Name $service -ErrorAction SilentlyContinue) { throw 'Desktop mode retained the Windows service' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut($startup)
    if ($shortcut.TargetPath -ne (Join-Path $app 'anilist-arr-sync.exe') -or !$shortcut.Arguments.Contains('--tray') -or !$shortcut.Arguments.Contains($data)) { throw 'Desktop startup shortcut lost tray/data arguments' }
    $shortcut = (New-Object -ComObject WScript.Shell).CreateShortcut("$desktop.lnk")
    if ($shortcut.TargetPath -ne (Join-Path $app 'anilist-arr-sync.exe') -or !$shortcut.Arguments.Contains('--tray --open-dashboard') -or !$shortcut.Arguments.Contains($data) -or (Test-Path -LiteralPath "$desktop.url")) {
        throw 'Tray desktop icon lost the installed launcher, browser option, or dashboard config path'
    }
    $stage = 'switch to manual startup with no desktop icon'
    Install '' '' 'manual'
    if (Get-Service -Name $service -ErrorAction SilentlyContinue) { throw 'Manual mode installed a Windows service' }
    if (Test-Path -LiteralPath @($startup, "$desktop.lnk", "$desktop.url") | Where-Object { $_ }) {
        throw 'Manual mode retained a startup or desktop shortcut'
    }
    $stage = 'restore desktop installation'
    Install '' 'desktop'
    $stage = 'tray startup'
    StartTray
    WaitHealthy
    $stage = 'tray icon recovery'
    $window = [FixtureTray]::AppWindow($tray.Id)
    if (![FixtureTray]::IconRegistered($window)) { throw 'Initialized tray icon is not registered with Explorer' }
    # Simulate Explorer losing this fixture icon, without restarting the user's
    # shell or disturbing other applications' notification icons.
    [FixtureTray]::RemoveIcon($window)
    if ([FixtureTray]::IconRegistered($window)) { throw 'Fixture icon removal did not reach Explorer' }
    $message = [FixtureTray]::RegisterWindowMessage('TaskbarCreated')
    if (!$message -or ![FixtureTray]::PostMessage($window, $message, [IntPtr]::Zero, [IntPtr]::Zero)) { throw 'Cannot deliver taskbar recreation notification' }
    $deadline = [DateTime]::UtcNow.AddSeconds(5)
    while (![FixtureTray]::IconRegistered($window) -and [DateTime]::UtcNow -lt $deadline) { Start-Sleep -Milliseconds 100 }
    if (![FixtureTray]::IconRegistered($window)) { throw 'Tray icon was not restored after taskbar recreation' }
    $stage = 'tray close'
    $window = [FixtureTray]::AppWindow($tray.Id)
    if ($window -eq [IntPtr]::Zero -or ![FixtureTray]::PostMessage($window, 0x0010, [IntPtr]::Zero, [IntPtr]::Zero)) { throw 'Cannot close the initialized tray window' }
    if (!$tray.WaitForExit(30000)) { throw 'Tray window did not drain the runtime on close' }
    if ($tray.ExitCode -ne 0) { throw "Tray exited with code $($tray.ExitCode)" }
    # Uninstall must also close an active tray itself, before deleting files.
    $stage = 'tray restart before uninstall'
    StartTray
    WaitHealthy
    $stage = 'uninstall with active tray'
    Uninstall
    if (!$tray.WaitForExit(30000)) { throw 'Uninstall left the tray runtime running' }
    $tray = $null; $trayOutput = $null; $trayError = $null
    if (Test-Path -LiteralPath (Join-Path $app 'update-owner')) { throw 'Uninstall retained ownership marker' }
    if (Test-Path -LiteralPath (Join-Path $app 'anilist-arr-sync.exe')) { throw 'Uninstall retained executable' }
    if (Test-Path -LiteralPath $startup) { throw 'Uninstall retained startup shortcut' }
    if (Test-Path -LiteralPath @("$desktop.lnk", "$desktop.url") | Where-Object { $_ }) { throw 'Uninstall retained desktop shortcut' }
    if (Test-Path -LiteralPath 'HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\AniList-Arr-Sync_is1') { throw 'Uninstall retained the shared installation record' }
    if (!(Test-Path -LiteralPath (Join-Path $data 'config.xml'))) { throw 'Uninstall deleted persistent user data' }

    # External storage/backups and link targets outside the chosen data directory must always survive.
    $outsideBackups = Join-Path $root 'outside-backups'
    $outsideLinkTarget = Join-Path $root 'outside-link-target'
    New-Item -ItemType Directory -Path $outsideBackups, (Join-Path $outsideLinkTarget 'nested') -Force | Out-Null
    $outsideBackupFile = Join-Path $outsideBackups 'external-backup.zip'
    $outsideTargetFile = Join-Path $outsideLinkTarget 'nested\must-survive.txt'
    Set-Content -LiteralPath $outsideBackupFile -Value 'external backup'
    Set-Content -LiteralPath $outsideTargetFile -Value 'outside junction target'

    # Opted-in deletion for a custom DATADIR must stop an active tray first, resolve installation.ini
    # before it is removed, delete the custom data directory, and avoid following links outside it.
    $stage = 'reinstall for opted-in custom data deletion'
    Install '' '' 'manual' $data
    New-Item -ItemType Directory -Path (Join-Path $data 'backups') -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $data 'backups\local-backup.zip') -Value 'local backup'
    New-Item -ItemType Junction -Path (Join-Path $data 'external-link') -Target $outsideLinkTarget | Out-Null
    New-Item -ItemType SymbolicLink -Path (Join-Path $data 'external-file-link.zip') -Target $outsideBackupFile | Out-Null
    StartTray
    WaitHealthy
    $stage = 'opted-in uninstall deleting custom data directory with active tray'
    Uninstall @('/DELETEAPPDATA')
    if (!$tray.WaitForExit(30000)) { throw 'Opted-in uninstall left the tray runtime running' }
    $tray = $null; $trayOutput = $null; $trayError = $null
    if (Test-Path -LiteralPath $data) { throw 'Opted-in uninstall left custom data directory behind' }
    if (!(Test-Path -LiteralPath $outsideBackupFile)) { throw 'Opted-in uninstall deleted external backup outside data directory' }
    if (!(Test-Path -LiteralPath $outsideTargetFile)) { throw 'Opted-in uninstall followed directory junction outside data directory' }

    # Opted-in deletion must also stop and remove a running Windows service before deleting data.
    $stage = 'reinstall service for opted-in deletion'
    Install '' 'service' '' $data
    Start-Service -Name $service
    WaitHealthy
    $stage = 'opted-in uninstall deleting custom data directory with active service'
    Uninstall @('/DELETEAPPDATA=1')
    if (Get-Service -Name $service -ErrorAction SilentlyContinue) { throw 'Opted-in uninstall retained the Windows service' }
    if (Test-Path -LiteralPath $data) { throw 'Opted-in service uninstall left custom data directory behind' }
    if (!(Test-Path -LiteralPath $outsideBackupFile)) { throw 'Opted-in service uninstall touched files outside data directory' }

    # Default data location (%ProgramData%\AniList-Arr-Sync): silent/default preservation, then opted-in deletion.
    $stage = 'install using default data location'
    $createdDefaultData = $true
    Install '' '' 'manual' ''
    if (!(Test-Path -LiteralPath $defaultData)) { throw 'Installer did not create the default data directory' }
    New-Item -ItemType Directory -Path (Join-Path $defaultData 'backups') -Force | Out-Null
    Set-Content -LiteralPath (Join-Path $defaultData 'config.xml') -Value '<config/>'
    Set-Content -LiteralPath (Join-Path $defaultData 'backups\default-backup.zip') -Value 'default backup'
    $stage = 'default silent uninstall preserves default data location'
    Uninstall
    if (!(Test-Path -LiteralPath (Join-Path $defaultData 'config.xml')) -or !(Test-Path -LiteralPath (Join-Path $defaultData 'backups\default-backup.zip'))) {
        throw 'Silent uninstall deleted default application data without explicit opt-in'
    }
    $stage = 'reinstall for explicit preserve parameter'
    Install '' '' 'manual' ''
    Uninstall @('/DELETEAPPDATA=0')
    if (!(Test-Path -LiteralPath (Join-Path $defaultData 'config.xml'))) {
        throw 'Uninstall with /DELETEAPPDATA=0 deleted default application data'
    }
    $stage = 'reinstall for missing installation.ini refusal'
    Install '' '' 'manual' ''
    Remove-Item -LiteralPath (Join-Path $app 'installation.ini') -Force
    Uninstall @('/DELETEAPPDATA')
    if (!(Test-Path -LiteralPath (Join-Path $defaultData 'config.xml'))) {
        throw 'Uninstall assumed default data directory when installation.ini was missing'
    }
    $missingIniLog = Get-Content -LiteralPath $lastInstallerLog -Raw
    if (!$missingIniLog.Contains('not deleted')) {
        throw 'Uninstall log did not report refusal when installation.ini was missing'
    }
    $stage = 'reinstall for opted-in default data deletion'
    Install '' '' 'manual' ''
    $stage = 'opted-in uninstall deleting default data location'
    Uninstall @('/DELETEAPPDATA=1')
    if (Test-Path -LiteralPath $defaultData) { throw 'Opted-in uninstall left default data directory behind' }
    if (!(Test-Path -LiteralPath $outsideBackupFile)) { throw 'Default data deletion touched files outside data directory' }

    # Reject unsafe deletion targets and report the path left behind without deleting anything unsafe.
    $unsafeLinkReal = Join-Path $root 'unsafe-link-real'
    $unsafeLink = Join-Path $root 'unsafe-link'
    New-Item -ItemType Directory -Path (Join-Path $unsafeLinkReal 'sub') -Force | Out-Null
    $unsafeLinkSentinel = Join-Path $unsafeLinkReal 'keep.txt'
    $unsafeSubSentinel = Join-Path $unsafeLinkReal 'sub\keep-sub.txt'
    Set-Content -LiteralPath $unsafeLinkSentinel -Value 'must survive'
    Set-Content -LiteralPath $unsafeSubSentinel -Value 'must survive sub'
    New-Item -ItemType Junction -Path $unsafeLink -Target $unsafeLinkReal | Out-Null
    $unsafeSeedData = Join-Path $root 'unsafe-seed-data'
    $appSentinel = Join-Path $app 'keep-in-app.txt'
    foreach ($unsafeCase in @(
        @{ Name = 'drive root'; Path = 'C:\'; MustExist = 'C:\' },
        @{ Name = 'shared ProgramData'; Path = $env:ProgramData; MustExist = $env:ProgramData },
        @{ Name = 'Windows directory'; Path = $env:SystemRoot; MustExist = $env:SystemRoot },
        @{ Name = 'installation directory'; Path = $app; MustExist = $appSentinel; CreateSentinel = $true },
        @{ Name = 'parent of installation directory'; Path = $root; MustExist = $outsideBackupFile },
        @{ Name = 'junction target'; Path = $unsafeLink; MustExist = $unsafeLinkSentinel },
        @{ Name = 'parent junction traversal'; Path = (Join-Path $unsafeLink 'sub'); MustExist = $unsafeSubSentinel }
    )) {
        $stage = "reject unsafe deletion target ($($unsafeCase.Name))"
        Install '' '' 'manual' $unsafeSeedData
        if ($unsafeCase.CreateSentinel) { Set-Content -LiteralPath $appSentinel -Value 'must survive' }
        Set-Content -LiteralPath (Join-Path $app 'installation.ini') -Value "[Installation]`r`nServiceName=$service`r`nDataDir=$($unsafeCase.Path)`r`n"
        Uninstall @('/DELETEAPPDATA')
        if (!(Test-Path -LiteralPath $unsafeCase.MustExist)) { throw "Unsafe target ($($unsafeCase.Name)) was deleted" }
        if ($unsafeCase.CreateSentinel -and (Test-Path -LiteralPath $appSentinel)) { Remove-Item -LiteralPath $appSentinel -Force }
        $logText = Get-Content -LiteralPath $lastInstallerLog -Raw
        if (!$logText.Contains($unsafeCase.Path) -or !$logText.Contains('not deleted')) {
            throw "Uninstall log did not clearly report refusal for $($unsafeCase.Name): $($unsafeCase.Path)"
        }
    }

    # Report deletion failures clearly, including the path left behind.
    $stage = 'report deletion failure with locked file'
    $lockedData = Join-Path $root 'locked-data'
    Install '' '' 'manual' $lockedData
    $lockedFile = Join-Path $lockedData 'locked.db'
    Set-Content -LiteralPath $lockedFile -Value 'locked'
    $lockStream = [IO.File]::Open($lockedFile, [IO.FileMode]::Open, [IO.FileAccess]::ReadWrite, [IO.FileShare]::None)
    try {
        Uninstall @('/DELETEAPPDATA')
    } finally {
        $lockStream.Dispose()
    }
    if (!(Test-Path -LiteralPath $lockedData)) { throw 'Locked data directory unexpectedly disappeared' }
    $lockedLogText = Get-Content -LiteralPath $lastInstallerLog -Raw
    if (!$lockedLogText.Contains($lockedData) -or !$lockedLogText.Contains('Could not')) {
        throw "Uninstall log did not report deletion failure and left-behind path: $lockedData"
    }
} catch {
    Write-Host "Installer fixture failed: mode=$env:RELEASE_MODE, arch=$Arch, stage=$stage"
    Write-Host $_.ScriptStackTrace
    $registered = Get-CimInstance Win32_Service -Filter "Name='$service'" -ErrorAction SilentlyContinue
    if ($registered) { Write-Host "Service state=$($registered.State), exit=$($registered.ExitCode), specificExit=$($registered.ServiceSpecificExitCode), pid=$($registered.ProcessId)" }
    if ($tray) {
        Write-Host "Tray pid=$($tray.Id), exited=$($tray.HasExited)"
        if (!$tray.HasExited) { $tray.Kill(); $tray.WaitForExit() }
        Write-Host "Tray exit code=$($tray.ExitCode)"
        foreach ($output in @($trayOutput, $trayError)) {
            if ($output) { $output.GetAwaiter().GetResult() -split "`n" | Select-Object -Last 10 | ForEach-Object { Write-Host $_ } }
        }
    }
    throw
} finally {
    if ($tray -and !$tray.HasExited) { Stop-Process -Id $tray.Id -Force }
    $remaining = Get-Service -Name $service -ErrorAction SilentlyContinue
    if ($remaining) {
        Stop-Service -Name $service -Force -ErrorAction SilentlyContinue
        & sc.exe delete $service | Out-Null
    }
    if (Test-Path -LiteralPath (Join-Path $app 'unins000.exe')) {
        $stage = 'cleanup uninstall'
        Uninstall
    }
    if ($createdDefaultData -and (Test-Path -LiteralPath $defaultData)) {
        Remove-Item -LiteralPath $defaultData -Recurse -Force
    }
    # Delete only the uniquely created fixture tree beneath this workspace.
    $resolved = [IO.Path]::GetFullPath($root)
    if ([IO.Path]::GetDirectoryName($resolved) -ne $releaseRoot -or [IO.Path]::GetFileName($resolved) -notlike 'install-test-*') { throw 'Unsafe fixture cleanup target' }
    if (Test-Path -LiteralPath $resolved) {
        Get-ChildItem -LiteralPath $resolved -Recurse -Force -ErrorAction SilentlyContinue |
            Where-Object { $_.Attributes -band [IO.FileAttributes]::ReparsePoint } |
            ForEach-Object {
                if ($_.PSIsContainer) { [IO.Directory]::Delete($_.FullName, $false) }
                else { [IO.File]::Delete($_.FullName) }
            }
        Remove-Item -LiteralPath $resolved -Recurse -Force
    }
}
