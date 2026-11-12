#ifndef Binary
  #error Binary is required
#endif
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
AppId=AniList-Arr-Sync-{#Arch}
AppName=AniList-Arr-Sync
AppVersion={#ReleaseVersion}
AppPublisher=AniList-Arr-Sync contributors
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
#ifdef Production
SignTool=signpath
SignedUninstaller=yes
SignedUninstallerDir={#OutputDir}\uninstallers-{#Arch}
#else
SignedUninstaller=no
#endif

[Types]
Name: "service"; Description: "Windows service (starts with Windows)"
Name: "desktop"; Description: "Desktop tray (starts at sign-in)"

[Components]
Name: "service"; Description: "Windows service"; Types: service; Flags: exclusive
Name: "desktop"; Description: "Desktop tray"; Types: desktop; Flags: exclusive

[Dirs]
Name: "{code:DataDir}"

[Files]
Source: "{#Binary}"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{group}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Components: desktop
Name: "{userstartup}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Components: desktop
Name: "{group}\Open dashboard"; Filename: "http://localhost:7171"

[UninstallDelete]
Type: files; Name: "{app}\update-owner"
Type: files; Name: "{app}\installation.ini"

[INI]
Filename: "{app}\installation.ini"; Section: "Installation"; Key: "ServiceName"; String: "{code:StoredServiceName}"
Filename: "{app}\installation.ini"; Section: "Installation"; Key: "DataDir"; String: "{code:DataDir}"

[Code]
type
  TServiceStatus = record
    ServiceType, CurrentState, ControlsAccepted, Win32ExitCode,
      ServiceSpecificExitCode, CheckPoint, WaitHint: LongWord;
  end;

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

procedure StopService;
var Manager, Service: THandle;
    Status: TServiceStatus;
    Attempts: Integer;
begin
  RunSC('stop ' + ServiceName, True);
  Manager := OpenSCManager('', '', 1);
  if Manager = 0 then RaiseException('Cannot open service manager');
  try
    Service := OpenService(Manager, ServiceName, 4);
    if Service = 0 then begin
      if DLLGetLastError <> 1060 then RaiseException('Cannot query service');
      Exit;
    end;
    try
      for Attempts := 1 to 300 do begin
        if not QueryServiceStatus(Service, Status) then RaiseException('Cannot query service state');
        if Status.CurrentState = 1 then Exit;
        Sleep(100);
      end;
      RaiseException('Timed out stopping service');
    finally
      CloseServiceHandle(Service);
    end;
  finally
    CloseServiceHandle(Manager);
  end;
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
begin
  Result := '';
  try
    StopTray;
    StopService;
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
    if WizardIsComponentSelected('desktop') then begin
      Account := GetEnv('USERDOMAIN') + '\' + GetUserNameString;
      if not Exec(ExpandConstant('{sys}\icacls.exe'), '"' + DataDir('') + '" /grant "' + Account + ':(OI)(CI)M"', '', SW_HIDE, ewWaitUntilTerminated, Code) or (Code <> 0) then
        RaiseException('Cannot grant the desktop account access to application data');
    end;
    if WizardIsComponentSelected('service') then begin
      Command := '"' + ExpandConstant('{app}\anilist-arr-sync.exe') + '" --no-browser --data-dir "' +
        DataDir('') + '" --config "' + DataDir('') + '\config.xml"';
      StringChangeEx(Command, '"', '\"', True);
      RunSC('create ' + ServiceName + ' type= own start= auto binPath= "' + Command + '" DisplayName= "AniList-Arr-Sync"', False);
      RunSC('failure ' + ServiceName + ' reset= 86400 actions= restart/60000/restart/60000/restart/60000', False);
      RunSC('start ' + ServiceName, False);
    end;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then begin
    StopTray;
    StopService;
    RunSC('delete ' + ServiceName, True);
  end;
end;
