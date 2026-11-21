#ifndef Binary
  #error Binary is required
#endif
#define DashboardURL "http://localhost:7171"
#define StartDescription "Start AniList-Arr-Sync"
#if Arch == "arm64"
  #define AllowedArch "arm64"
#else
  #define AllowedArch "x64compatible"
#endif
#if Pos("-", ReleaseVersion) > 0
  #define NumericVersion Copy(ReleaseVersion, 2, Pos("-", ReleaseVersion) - 2)
#else
  #define NumericVersion Copy(ReleaseVersion, 2)
#endif
[Setup]
AppId=AniList-Arr-Sync
AppName=AniList-Arr-Sync
AppVersion={#ReleaseVersion}
AppPublisher=AniList-Arr-Sync contributors
AppCopyright=Copyright (C) 2026 AniList-Arr-Sync contributors
LicenseFile=..\LICENSE
AppPublisherURL=https://github.com/lucasskywalker64/anilist-arr-sync
VersionInfoProductName=AniList-Arr-Sync
VersionInfoVersion={#NumericVersion}.0
VersionInfoTextVersion={#ReleaseVersion}
DefaultDirName={autopf}\AniList-Arr-Sync
DefaultGroupName=AniList-Arr-Sync
PrivilegesRequired=admin
ArchitecturesAllowed={#AllowedArch}
ArchitecturesInstallIn64BitMode={#AllowedArch}
OutputDir={#OutputDir}
OutputBaseFilename=anilist-arr-sync_{#ReleaseVersion}_windows_{#Arch}_setup
Compression=lzma2
SolidCompression=yes
CloseApplications=yes
RestartApplications=no
WizardStyle=classic
WizardSmallImageFile=compiler:WizClassicSmallImage.bmp
#ifdef Production
SignTool=signpath
SignedUninstaller=yes
SignedUninstallerDir={#OutputDir}\uninstallers-{#Arch}
#else
SignedUninstaller=no
#endif

[Messages]
SelectTasksLabel2=Select the additional tasks you would like Setup to perform while installing [name], then click Next.

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop icon"
Name: "service"; Description: "Install Windows Service (Starts when the computer starts and runs as LocalSystem, even when no one is signed in)"; GroupDescription: "Start automatically"; Flags: exclusive unchecked
Name: "startup"; Description: "Create shortcut in Startup folder (Starts the tray app when you log into Windows)"; GroupDescription: "Start automatically"; Flags: exclusive
Name: "manual"; Description: "Do not start automatically"; GroupDescription: "Start automatically"; Flags: exclusive unchecked

[Dirs]
Name: "{code:DataDir}"

[Files]
Source: "{#Binary}"; DestDir: "{app}"; DestName: "anilist-arr-sync.exe"; Flags: ignoreversion
Source: "..\LICENSE"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: not service
Name: "{userstartup}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: startup
Name: "{commondesktop}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: desktopicon and not service
Name: "{commondesktop}\AniList-Arr-Sync"; Filename: "{#DashboardURL}"; Tasks: desktopicon and service
Name: "{group}\Open dashboard"; Filename: "{#DashboardURL}"

[InstallDelete]
Type: files; Name: "{userstartup}\AniList-Arr-Sync.lnk"; Tasks: not startup
Type: files; Name: "{group}\AniList-Arr-Sync.lnk"; Tasks: service
Type: files; Name: "{commondesktop}\AniList-Arr-Sync.lnk"; Tasks: not desktopicon or service
Type: files; Name: "{commondesktop}\AniList-Arr-Sync.url"; Tasks: not desktopicon or not service

[Run]
Filename: "{#DashboardURL}"; Description: "{#StartDescription}"; Tasks: service; BeforeInstall: StartInstalledService; Flags: postinstall shellexec skipifsilent runasoriginaluser
Filename: "{app}\anilist-arr-sync.exe"; Description: "{#StartDescription}"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: not service; Flags: postinstall nowait skipifsilent runasoriginaluser

[UninstallDelete]
Type: files; Name: "{app}\update-owner"
Type: files; Name: "{app}\installation.ini"

[INI]
Filename: "{app}\installation.ini"; Section: "Installation"; Key: "ServiceName"; String: "{code:StoredServiceName}"
Filename: "{app}\installation.ini"; Section: "Installation"; Key: "DataDir"; String: "{code:DataDir}"

[Code]
const
  ServiceStopped = 1;
  ServiceStartPending = 2;
  ServiceStopPending = 3;
  ServiceRunning = 4;

type
  TServiceStatus = record
    ServiceType, CurrentState, ControlsAccepted, Win32ExitCode,
      ServiceSpecificExitCode, CheckPoint, WaitHint: LongWord;
  end;

var
  StartupTasksInitialized: Boolean;
  ResumeService: Boolean;

function OpenSCManager(Machine, Database: String; Access: LongWord): THandle;
  external 'OpenSCManagerW@advapi32.dll stdcall';
function OpenService(Manager: THandle; Name: String; Access: LongWord): THandle;
  external 'OpenServiceW@advapi32.dll stdcall';
function QueryServiceStatus(Service: THandle; var Status: TServiceStatus): Boolean;
  external 'QueryServiceStatus@advapi32.dll stdcall';
function CloseServiceHandle(Handle: THandle): Boolean;
  external 'CloseServiceHandle@advapi32.dll stdcall';
function FindWindowEx(Parent, After: THandle; ClassName: String; WindowName: THandle): THandle;
  external 'FindWindowExW@user32.dll stdcall';
function GetWindowThreadProcessId(Window: THandle; var ProcessId: LongWord): LongWord;
  external 'GetWindowThreadProcessId@user32.dll stdcall';
function OpenProcess(Access: LongWord; Inherit: Boolean; ProcessId: LongWord): THandle;
  external 'OpenProcess@kernel32.dll stdcall';
function QueryFullProcessImageName(Process: THandle; Flags: LongWord; FileName: String; var Size: LongWord): Boolean;
  external 'QueryFullProcessImageNameW@kernel32.dll stdcall';
function PostMessage(Window: THandle; Message: LongWord; WParam, LParam: THandle): Boolean;
  external 'PostMessageW@user32.dll stdcall';
function WaitForSingleObject(Handle: THandle; Timeout: LongWord): LongWord;
  external 'WaitForSingleObject@kernel32.dll stdcall';
function CloseHandle(Handle: THandle): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';

function ServiceName: String;
begin
  Result := ExpandConstant('{param:SERVICENAME|}');
  if Result = '' then Result := GetIniString('Installation', 'ServiceName', 'AniListArrSync', ExpandConstant('{app}\installation.ini'));
end;

procedure CurPageChanged(CurPageID: Integer);
var LegacyType: String;
begin
  if (CurPageID <> wpSelectTasks) or StartupTasksInitialized then Exit;
  StartupTasksInitialized := True;
  LegacyType := Lowercase(ExpandConstant('{param:TYPE|}'));
  if LegacyType = 'service' then WizardSelectTasks('service')
  else if LegacyType = 'desktop' then WizardSelectTasks('startup')
  else if (GetPreviousData('UsesStartupTasks', '') = '') and
    (ExpandConstant('{param:TASKS|}{param:MERGETASKS|}') = '') then begin
    if FileExists(ExpandConstant('{app}\installation.ini')) then begin
      if RegKeyExists(HKLM, 'SYSTEM\CurrentControlSet\Services\' + ServiceName) then WizardSelectTasks('service');
    end else if WizardSilent then WizardSelectTasks('service');
  end;
end;

procedure RegisterPreviousData(PreviousDataKey: Integer);
begin
  SetPreviousData(PreviousDataKey, 'UsesStartupTasks', '1');
end;

function StoredServiceName(Param: String): String;
begin
  Result := ServiceName;
end;

function DataDir(Param: String): String;
begin
  Result := ExpandConstant('{param:DATADIR|}');
  if Result = '' then Result := GetIniString('Installation', 'DataDir', ExpandConstant('{commonappdata}\AniList-Arr-Sync'), ExpandConstant('{app}\installation.ini'));
end;

procedure RunSC(Arguments: String; AllowMissing: Boolean);
var Code: Integer;
begin
  if not Exec(ExpandConstant('{sys}\sc.exe'), Arguments, '', SW_HIDE, ewWaitUntilTerminated, Code) then
    RaiseException('Cannot run Windows service control');
  if (Code <> 0) and not (AllowMissing and ((Code = 1060) or (Code = 1062))) then
    RaiseException('Service control failed: ' + IntToStr(Code));
end;

function ChangeServiceState(TargetState: LongWord): Boolean;
var Manager, Service: THandle;
    Status: TServiceStatus;
    Attempts: Integer;
begin
  Result := False;
  Manager := OpenSCManager('', '', 1);
  if Manager = 0 then RaiseException('Cannot open service manager');
  try
    Service := OpenService(Manager, ServiceName, 4);
    if Service = 0 then begin
      if (DLLGetLastError = 1060) and (TargetState = ServiceStopped) then Exit;
      RaiseException('Cannot query service');
    end;
    try
      if not QueryServiceStatus(Service, Status) then RaiseException('Cannot query service state');
      Result := (Status.CurrentState = ServiceRunning) or (Status.CurrentState = ServiceStartPending);
      if Status.CurrentState = TargetState then Exit;
      if TargetState = ServiceRunning then begin
        if Status.CurrentState <> ServiceStartPending then RunSC('start ' + ServiceName, False);
      end else if Status.CurrentState <> ServiceStopPending then
        RunSC('stop ' + ServiceName, True);
      for Attempts := 1 to 300 do begin
        if not QueryServiceStatus(Service, Status) then RaiseException('Cannot query service state');
        if Status.CurrentState = TargetState then Exit;
        if (TargetState = ServiceRunning) and (Status.CurrentState = ServiceStopped) then
          RaiseException('AniList-Arr-Sync stopped before its dashboard was ready. Service exit code: ' + IntToStr(Status.Win32ExitCode));
        Sleep(100);
      end;
      RaiseException('Timed out waiting for the AniList-Arr-Sync service');
    finally
      CloseServiceHandle(Service);
    end;
  finally
    CloseServiceHandle(Manager);
  end;
end;

procedure StartInstalledService;
begin
  { The app reports SERVICE_RUNNING after its HTTP listener is ready. }
  ChangeServiceState(ServiceRunning);
end;

procedure StopTray;
var Window, Process: THandle;
    ProcessId, Size: LongWord;
    FileName: String;
begin
  Window := FindWindowEx(0, 0, 'AniListArrSyncTray', 0);
  while Window <> 0 do begin
    GetWindowThreadProcessId(Window, ProcessId);
    Process := OpenProcess($00101000, False, ProcessId);
    if Process <> 0 then begin
      try
        Size := 32768;
        SetLength(FileName, Size);
        if QueryFullProcessImageName(Process, 0, FileName, Size) then begin
          SetLength(FileName, Size);
          if CompareText(FileName, ExpandConstant('{app}\anilist-arr-sync.exe')) = 0 then begin
            if not PostMessage(Window, $0010, 0, 0) then RaiseException('Cannot close the installed tray application');
            if WaitForSingleObject(Process, 30000) <> 0 then RaiseException('Timed out closing the installed tray application');
          end;
        end;
      finally
        CloseHandle(Process);
      end;
    end;
    Window := FindWindowEx(0, Window, 'AniListArrSyncTray', 0);
  end;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
var Window: THandle;
    ProcessId: LongWord;
    Command: String;
    Code: Integer;
begin
  Result := '';
  try
    if not WizardIsTaskSelected('service') then begin
      { User Startup and the data ACL must belong to the actual desktop user.
        Refuse credential-based elevation instead of configuring the admin's
        profile. Service installation does not depend on a desktop account. }
      Window := FindWindowEx(0, 0, 'Shell_TrayWnd', 0);
      if Window = 0 then RaiseException('Cannot identify the signed-in desktop account. Use Windows service mode.');
      GetWindowThreadProcessId(Window, ProcessId);
      Command := '$ErrorActionPreference=''Stop''; ' +
        '$process=Get-CimInstance Win32_Process -Filter ''ProcessId=' + IntToStr(ProcessId) + '''; ' +
        '$owner=Invoke-CimMethod -InputObject $process -MethodName GetOwner; ' +
        'if ($owner.ReturnValue -ne 0) { exit 1 }; ' +
        '$account=[Security.Principal.NTAccount]::new($owner.Domain,$owner.User); ' +
        '$sid=$account.Translate([Security.Principal.SecurityIdentifier]).Value; ' +
        'if ($sid -ne [Security.Principal.WindowsIdentity]::GetCurrent().User.Value) { exit 1 }';
      if not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
        '-NoProfile -NonInteractive -Command "' + Command + '"', '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then
        RaiseException('Desktop mode requires the signed-in desktop account to be an administrator. Use Windows service mode when elevating with another account.');
    end;
    StopTray;
    ResumeService := ChangeServiceState(ServiceStopped);
  except
    Result := GetExceptionMessage;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var Owner, Marker, Command, Account: String;
    Code: Integer;
begin
  if CurStep = ssPostInstall then begin
    Owner := ExpandConstant('{param:UPDATEOWNER|}');
    Marker := ExpandConstant('{app}\update-owner');
    { Preserve existing ownership on upgrade. Direct installs create no marker. }
    if Owner <> '' then
      if not SaveStringToFile(Marker, Owner + #13#10, False) then
        RaiseException('Cannot write update ownership marker');
    RunSC('delete ' + ServiceName, True);
    if not WizardIsTaskSelected('service') then begin
      Account := GetEnv('USERDOMAIN') + '\' + GetUserNameString;
      if not Exec(ExpandConstant('{sys}\icacls.exe'), '"' + DataDir('') + '" /grant "' + Account + ':(OI)(CI)M"', '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then
        RaiseException('Cannot grant the desktop account access to application data');
    end else begin
      Command := '"' + ExpandConstant('{app}\anilist-arr-sync.exe') + '" --no-browser --data-dir "' +
        DataDir('') + '" --config "' + DataDir('') + '\config.xml"';
      StringChangeEx(Command, '"', '\"', True);
      RunSC('create ' + ServiceName + ' type= own start= auto binPath= "' + Command + '" DisplayName= "AniList-Arr-Sync"', False);
      RunSC('failure ' + ServiceName + ' reset= 86400 actions= restart/60000/restart/60000/restart/60000', False);
      if WizardSilent and ResumeService then StartInstalledService;
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then begin
    StopTray;
    ChangeServiceState(ServiceStopped);
    RunSC('delete ' + ServiceName, True);
  end;
end;
