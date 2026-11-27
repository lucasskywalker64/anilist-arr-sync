#ifndef Binary
  #error Binary is required
#endif
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
Name: "{commondesktop}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--tray --open-dashboard --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: desktopicon and not service
Name: "{commondesktop}\AniList-Arr-Sync"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--open-service-dashboard ""{code:StoredServiceName}"" ""{code:DataDir}\config.xml"""; Tasks: desktopicon and service
Name: "{group}\Open dashboard"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--open-installed-dashboard ""{code:DataDir}\config.xml"""; Tasks: not service
Name: "{group}\Open dashboard"; Filename: "{app}\anilist-arr-sync.exe"; Parameters: "--open-installed-dashboard ""{code:DataDir}\config.xml"" ""{code:StoredServiceName}"""; Tasks: service

[InstallDelete]
Type: files; Name: "{userstartup}\AniList-Arr-Sync.lnk"; Tasks: not startup
Type: files; Name: "{group}\AniList-Arr-Sync.lnk"; Tasks: service
Type: files; Name: "{commondesktop}\AniList-Arr-Sync.lnk"; Tasks: not desktopicon
Type: files; Name: "{commondesktop}\AniList-Arr-Sync.url"
Type: files; Name: "{group}\Open dashboard.url"

[Run]
Filename: "{app}\anilist-arr-sync.exe"; Description: "{#StartDescription}"; Parameters: "--open-installed-dashboard ""{code:DataDir}\config.xml"" ""{code:StoredServiceName}"""; Tasks: service; BeforeInstall: StartInstalledService; Flags: postinstall nowait skipifsilent runasoriginaluser
Filename: "{app}\anilist-arr-sync.exe"; Description: "{#StartDescription}"; Parameters: "--tray --open-dashboard --data-dir ""{code:DataDir}"" --config ""{code:DataDir}\config.xml"""; Tasks: not service; Flags: postinstall nowait skipifsilent runasoriginaluser

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
  InvalidFileAttributes = $FFFFFFFF;
  FileAttributeDirectory = $00000010;
  FileAttributeReparsePoint = $00000400;
  FileShareAll = $00000007;
  OpenExisting = 3;
  FileFlagBackupSemantics = $02000000;
  ErrorFileNotFound = 2;
  ErrorPathNotFound = 3;
  ErrorRedirectionNotAllowed = 448;

type
  TServiceStatus = record
    ServiceType, CurrentState, ControlsAccepted, Win32ExitCode,
      ServiceSpecificExitCode, CheckPoint, WaitHint: LongWord;
  end;

var
  StartupTasksInitialized: Boolean;
  ResumeService: Boolean;
  UninstallDataDir: String;
  DeleteApplicationDataRequested: Boolean;

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
function GetFileAttributes(FileName: String): LongWord;
  external 'GetFileAttributesW@kernel32.dll stdcall';
function CreateFile(FileName: String; DesiredAccess, ShareMode: LongWord; SecurityAttributes: THandle; CreationDisposition, FlagsAndAttributes: LongWord; TemplateFile: THandle): THandle;
  external 'CreateFileW@kernel32.dll stdcall';
function GetFinalPathNameByHandle(FileHandle: THandle; FilePath: String; FilePathSize, Flags: LongWord): LongWord;
  external 'GetFinalPathNameByHandleW@kernel32.dll stdcall';
function GetLongPathName(ShortPath, LongPath: String; BufferSize: LongWord): LongWord;
  external 'GetLongPathNameW@kernel32.dll stdcall';
function ExpandEnvironmentStrings(Src, Dst: String; Size: LongWord): LongWord;
  external 'ExpandEnvironmentStringsW@kernel32.dll stdcall';

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

function ResolveUninstallDataDir: String;
var IniPath: String;
begin
  Result := '';
  IniPath := ExpandConstant('{app}\installation.ini');
  if FileExists(IniPath) and IniKeyExists('Installation', 'DataDir', IniPath) then
    Result := Trim(GetIniString('Installation', 'DataDir', '', IniPath));
end;

function HasDeleteAppDataParam: Boolean;
var I: Integer;
    Arg, LowerArg, Value: String;
begin
  Result := False;
  for I := 1 to ParamCount do begin
    Arg := Trim(ParamStr(I));
    LowerArg := Lowercase(Arg);
    if LowerArg = '/deleteappdata' then
      Result := True
    else if Pos('/deleteappdata=', LowerArg) = 1 then begin
      Value := Trim(Copy(LowerArg, 16, Length(LowerArg)));
      if (Value = '1') or (Value = 'true') or (Value = 'yes') then
        Result := True
      else if (Value = '0') or (Value = 'false') or (Value = 'no') then
        Result := False;
    end;
  end;
end;

function StripTrailingBackslashes(Path: String): String;
begin
  Result := Trim(Path);
  StringChangeEx(Result, '/', '\', True);
  while (Length(Result) > 3) and (Result[Length(Result)] = '\') do
    SetLength(Result, Length(Result) - 1);
end;

function NormalizeLocalDrivePath(RawPath: String; var Normalized, Reason: String): Boolean;
var Path, Segment, DriveLetter: String;
    I, SegStart: Integer;
begin
  Result := False;
  Normalized := '';
  Path := Trim(RawPath);
  if Path = '' then begin
    Reason := 'The installation record (installation.ini) does not contain a recorded data directory.';
    Exit;
  end;
  StringChangeEx(Path, '/', '\', True);
  for I := 1 to Length(Path) do begin
    if (Path[I] = '*') or (Path[I] = '?') or (Path[I] = '"') or (Path[I] = '<') or (Path[I] = '>') or (Path[I] = '|') then begin
      Reason := 'The recorded data directory contains invalid path characters.';
      Exit;
    end;
  end;
  if Pos('\\', Path) > 0 then begin
    Reason := 'The recorded data directory must be a local drive path.';
    Exit;
  end;
  if Length(Path) < 3 then begin
    Reason := 'The recorded data directory must be an absolute local drive path.';
    Exit;
  end;
  DriveLetter := Uppercase(Copy(Path, 1, 1));
  if (DriveLetter[1] < 'A') or (DriveLetter[1] > 'Z') or (Path[2] <> ':') or (Path[3] <> '\') then begin
    Reason := 'The recorded data directory must be an absolute local drive path.';
    Exit;
  end;
  while (Length(Path) > 3) and (Path[Length(Path)] = '\') do
    SetLength(Path, Length(Path) - 1);
  if Length(Path) <= 3 then begin
    Reason := 'Refusing to delete a drive root.';
    Exit;
  end;
  if Pos(':', Copy(Path, 3, Length(Path))) > 0 then begin
    Reason := 'The recorded data directory contains an invalid colon.';
    Exit;
  end;
  SegStart := 4;
  for I := 4 to Length(Path) + 1 do begin
    if (I > Length(Path)) or (Path[I] = '\') then begin
      Segment := Copy(Path, SegStart, I - SegStart);
      if (Segment = '') or (Segment = '.') or (Segment = '..') then begin
        Reason := 'The recorded data directory contains relative or invalid path segments.';
        Exit;
      end;
      SegStart := I + 1;
    end;
  end;
  Normalized := Path;
  Result := True;
end;

function IsSameOrPrefix(Prefix, Target: String): Boolean;
var NormPrefix, NormTarget: String;
begin
  Result := False;
  NormPrefix := StripTrailingBackslashes(Prefix);
  NormTarget := StripTrailingBackslashes(Target);
  if (NormPrefix = '') or (NormTarget = '') then Exit;
  if CompareText(NormPrefix, NormTarget) = 0 then
    Result := True
  else if Pos(Lowercase(NormPrefix) + '\', Lowercase(NormTarget) + '\') = 1 then
    Result := True;
end;

function IsSameOrAncestor(Candidate, ProtectedPath: String): Boolean;
begin
  Result := IsSameOrPrefix(Candidate, ProtectedPath);
end;

function IsSameOrDescendant(Candidate, ProtectedPath: String): Boolean;
begin
  Result := IsSameOrPrefix(ProtectedPath, Candidate);
end;

function ExpandEnvVars(Value: String): String;
var Expanded: String;
    Len: LongWord;
begin
  Result := Value;
  if Pos('%', Value) = 0 then Exit;
  SetLength(Expanded, 32768);
  Len := ExpandEnvironmentStrings(Value, Expanded, 32768);
  if (Len > 1) and (Len <= 32768) then begin
    SetLength(Expanded, Len - 1);
    Result := Expanded;
  end;
end;

function IsDirectChildOf(Candidate, ParentDir: String): Boolean;
var NormParent, Rest: String;
begin
  Result := False;
  NormParent := StripTrailingBackslashes(ParentDir);
  if NormParent = '' then Exit;
  if Pos(Lowercase(NormParent) + '\', Lowercase(Candidate)) <> 1 then Exit;
  Rest := Copy(Candidate, Length(NormParent) + 2, Length(Candidate));
  Result := (Rest <> '') and (Pos('\', Rest) = 0);
end;

function IsRegisteredProfileRootOrAncestor(Candidate: String): Boolean;
var ProfilesKey, ProfilesDir, ProfilePath: String;
    Subkeys: TArrayOfString;
    I: Integer;
begin
  Result := False;
  ProfilesKey := 'SOFTWARE\Microsoft\Windows NT\CurrentVersion\ProfileList';
  if RegQueryStringValue(HKLM, ProfilesKey, 'ProfilesDirectory', ProfilesDir) then begin
    ProfilesDir := ExpandEnvVars(ProfilesDir);
    if IsSameOrAncestor(Candidate, ProfilesDir) or IsDirectChildOf(Candidate, ProfilesDir) then begin
      Result := True;
      Exit;
    end;
  end;
  if RegGetSubkeyNames(HKLM, ProfilesKey, Subkeys) then begin
    for I := 0 to GetArrayLength(Subkeys) - 1 do begin
      if RegQueryStringValue(HKLM, ProfilesKey + '\' + Subkeys[I], 'ProfileImagePath', ProfilePath) then begin
        if IsSameOrAncestor(Candidate, ExpandEnvVars(ProfilePath)) then begin
          Result := True;
          Exit;
        end;
      end;
    end;
  end;
end;

function IsUnsafeDataDirPath(Normalized: String; var Reason: String): Boolean;
var DriveRoot: String;
begin
  Result := True;
  if Length(Normalized) <= 3 then begin
    Reason := 'Refusing to delete a drive root.';
    Exit;
  end;
  DriveRoot := Copy(Normalized, 1, 3);
  if IsSameOrDescendant(Normalized, ExpandConstant('{win}')) or
     IsSameOrDescendant(Normalized, GetEnv('SystemRoot')) or
     IsSameOrDescendant(Normalized, ExpandConstant('{sys}')) or
     IsSameOrDescendant(Normalized, ExpandConstant('{commonappdata}\Microsoft')) or
     IsSameOrDescendant(Normalized, DriveRoot + 'Windows') or
     IsSameOrDescendant(Normalized, DriveRoot + '$Recycle.Bin') or
     IsSameOrDescendant(Normalized, DriveRoot + 'System Volume Information') or
     IsSameOrDescendant(Normalized, DriveRoot + 'Recovery') then begin
    Reason := 'Refusing to delete a Windows system directory.';
    Exit;
  end;
  if IsSameOrAncestor(Normalized, ExpandConstant('{app}')) then begin
    Reason := 'Refusing to delete the installation directory or its parent.';
    Exit;
  end;
  if IsRegisteredProfileRootOrAncestor(Normalized) or
     IsDirectChildOf(Normalized, DriveRoot + 'Users') then begin
    Reason := 'Refusing to delete a user profile directory.';
    Exit;
  end;
  if IsSameOrAncestor(Normalized, ExpandConstant('{win}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{sys}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonappdata}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonpf}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonpf32}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonpf64}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commoncf}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commoncf32}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commoncf64}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userappdata}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{localappdata}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userdocs}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commondocs}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userdesktop}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commondesktop}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userprograms}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonprograms}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userstartup}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonstartup}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{userstartmenu}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{commonstartmenu}')) or
     IsSameOrAncestor(Normalized, ExpandConstant('{tmp}')) or
     IsSameOrAncestor(Normalized, GetEnv('USERPROFILE')) or
     IsSameOrAncestor(Normalized, GetEnv('PUBLIC')) or
     IsSameOrAncestor(Normalized, GetEnv('ALLUSERSPROFILE')) or
     IsSameOrAncestor(Normalized, GetEnv('ProgramData')) or
     IsSameOrAncestor(Normalized, GetEnv('ProgramFiles')) or
     IsSameOrAncestor(Normalized, GetEnv('ProgramFiles(x86)')) or
     IsSameOrAncestor(Normalized, GetEnv('ProgramW6432')) or
     IsSameOrAncestor(Normalized, GetEnv('TEMP')) or
     IsSameOrAncestor(Normalized, GetEnv('TMP')) or
     IsSameOrAncestor(Normalized, DriveRoot + 'Users') or
     IsSameOrAncestor(Normalized, DriveRoot + 'ProgramData') or
     IsSameOrAncestor(Normalized, DriveRoot + 'Program Files') or
     IsSameOrAncestor(Normalized, DriveRoot + 'Program Files (x86)') or
     IsSameOrAncestor(Normalized, DriveRoot + 'Documents and Settings') or
     IsSameOrAncestor(Normalized, DriveRoot + 'PerfLogs') then begin
    Reason := 'Refusing to delete a shared or system directory.';
    Exit;
  end;
  Result := False;
end;

procedure ReportDataDeletionFailure(MessageText: String);
begin
  Log(MessageText);
  SuppressibleMsgBox(MessageText, mbError, MB_OK, IDOK);
end;

procedure ReportWin32PathError(ActionLabel, TargetPath: String; LastErr: LongWord);
begin
  if LastErr = ErrorRedirectionNotAllowed then
    ReportDataDeletionFailure('Application data was not deleted because the path traverses a symbolic link or junction:' + #13#10 + TargetPath)
  else
    ReportDataDeletionFailure('Could not delete application data. Failed to ' + ActionLabel + ' directory (error ' + IntToStr(LastErr) + '):' + #13#10 + TargetPath);
end;

function ValidateSafeLocalPath(CandidatePath, ReportedPath, LabelPrefix: String; var Normalized: String): Boolean;
var Reason, DisplayPath: String;
begin
  Result := False;
  if not NormalizeLocalDrivePath(CandidatePath, Normalized, Reason) then begin
    ReportDataDeletionFailure('Application data was not deleted because ' + LabelPrefix + ' is unsafe:' + #13#10 + ReportedPath + #13#10#13#10 + Reason);
    Exit;
  end;
  DisplayPath := ReportedPath;
  if DisplayPath = '' then DisplayPath := Normalized;
  if IsUnsafeDataDirPath(Normalized, Reason) then begin
    ReportDataDeletionFailure('Application data was not deleted because ' + LabelPrefix + ' is unsafe:' + #13#10 + DisplayPath + #13#10#13#10 + Reason);
    Exit;
  end;
  Result := True;
end;

function PromptDeleteApplicationData(DataPath: String; var DeleteData: Boolean): Boolean;
var Form: TSetupForm;
    IntroLabel, NoteLabel: TNewStaticText;
    PathEdit: TNewEdit;
    DeleteCheckBox: TNewCheckBox;
    UninstallButton, CancelButton: TNewButton;
    Margin, ContentWidth, ButtonWidth, ButtonHeight, Y: Integer;
begin
  Result := False;
  Form := CreateCustomForm(440, 240, False, True);
  try
    Form.Caption := 'Uninstall AniList-Arr-Sync';
    Margin := ScaleX(16);
    ContentWidth := Form.ClientWidth - (Margin * 2);
    Y := ScaleY(14);

    IntroLabel := TNewStaticText.Create(Form);
    IntroLabel.Parent := Form;
    IntroLabel.Left := Margin;
    IntroLabel.Top := Y;
    IntroLabel.Width := ContentWidth;
    IntroLabel.WordWrap := True;
    IntroLabel.AutoSize := True;
    IntroLabel.Caption := 'AniList-Arr-Sync preserves your application data by default. Recorded application data directory:';
    Y := IntroLabel.Top + IntroLabel.Height + ScaleY(8);

    PathEdit := TNewEdit.Create(Form);
    PathEdit.Parent := Form;
    PathEdit.Left := Margin;
    PathEdit.Top := Y;
    PathEdit.Width := ContentWidth;
    PathEdit.Height := ScaleY(23);
    PathEdit.ReadOnly := True;
    PathEdit.Color := clBtnFace;
    if DataPath <> '' then
      PathEdit.Text := DataPath
    else
      PathEdit.Text := '(not recorded in installation.ini)';
    Y := PathEdit.Top + PathEdit.Height + ScaleY(12);

    DeleteCheckBox := TNewCheckBox.Create(Form);
    DeleteCheckBox.Parent := Form;
    DeleteCheckBox.Left := Margin;
    DeleteCheckBox.Top := Y;
    DeleteCheckBox.Width := ContentWidth;
    DeleteCheckBox.Height := ScaleY(20);
    DeleteCheckBox.Caption := '&Delete application data';
    DeleteCheckBox.Enabled := DataPath <> '';
    DeleteCheckBox.Checked := DeleteData and DeleteCheckBox.Enabled;
    Y := DeleteCheckBox.Top + DeleteCheckBox.Height + ScaleY(6);

    NoteLabel := TNewStaticText.Create(Form);
    NoteLabel.Parent := Form;
    NoteLabel.Left := Margin + ScaleX(18);
    NoteLabel.Top := Y;
    NoteLabel.Width := ContentWidth - ScaleX(18);
    NoteLabel.WordWrap := True;
    NoteLabel.AutoSize := True;
    NoteLabel.Caption :=
      'Permanently deletes configuration, credentials, the database, backups, and any other files inside the directory above.' + #13#10#13#10 +
      'This affects local application data only. It does not undo changes in Sonarr, Radarr, or AniList.';
    Y := NoteLabel.Top + NoteLabel.Height + ScaleY(16);

    ButtonHeight := ScaleY(23);
    ButtonWidth := Form.CalculateButtonWidth(['&Uninstall', 'Cancel']);

    CancelButton := TNewButton.Create(Form);
    CancelButton.Parent := Form;
    CancelButton.Caption := 'Cancel';
    CancelButton.ModalResult := mrCancel;
    CancelButton.Cancel := True;
    CancelButton.Width := ButtonWidth;
    CancelButton.Height := ButtonHeight;
    CancelButton.Left := Form.ClientWidth - Margin - ButtonWidth;
    CancelButton.Top := Y;

    UninstallButton := TNewButton.Create(Form);
    UninstallButton.Parent := Form;
    UninstallButton.Caption := '&Uninstall';
    UninstallButton.ModalResult := mrOk;
    UninstallButton.Default := True;
    UninstallButton.Width := ButtonWidth;
    UninstallButton.Height := ButtonHeight;
    UninstallButton.Left := CancelButton.Left - ScaleX(8) - ButtonWidth;
    UninstallButton.Top := Y;

    Form.ClientHeight := Y + ButtonHeight + ScaleY(14);
    Form.ActiveControl := UninstallButton;
    Form.FlipAndCenterIfNeeded(True, nil, False);

    if Form.ShowModal = mrOk then begin
      DeleteData := DeleteCheckBox.Enabled and DeleteCheckBox.Checked;
      Result := True;
    end;
  finally
    Form.Free;
  end;
end;

procedure DeleteVerifiedApplicationData(RawPath: String);
var NormalizedPath, FinalPath, NormalizedFinal, LongPath: String;
    Attrs, LastErr, Len: LongWord;
    Handle: THandle;
    Deleted: Boolean;
begin
  if not ValidateSafeLocalPath(RawPath, RawPath, 'the recorded data directory', NormalizedPath) then
    Exit;
  Attrs := GetFileAttributes(NormalizedPath);
  if Attrs = InvalidFileAttributes then begin
    LastErr := DLLGetLastError;
    if (LastErr = ErrorFileNotFound) or (LastErr = ErrorPathNotFound) then begin
      Log('Application data directory does not exist, nothing to delete: ' + NormalizedPath);
      Exit;
    end;
    ReportWin32PathError('inspect', NormalizedPath, LastErr);
    Exit;
  end;
  if (Attrs and FileAttributeReparsePoint) <> 0 then begin
    ReportDataDeletionFailure('Application data was not deleted because the target directory is a symbolic link or junction:' + #13#10 + NormalizedPath);
    Exit;
  end;
  if (Attrs and FileAttributeDirectory) = 0 then begin
    ReportDataDeletionFailure('Application data was not deleted because the recorded path is not a directory:' + #13#10 + NormalizedPath);
    Exit;
  end;
  Handle := CreateFile(NormalizedPath, 0, FileShareAll, 0, OpenExisting, FileFlagBackupSemantics, 0);
  if Handle = THandle(-1) then begin
    ReportWin32PathError('verify', NormalizedPath, DLLGetLastError);
    Exit;
  end;
  try
    SetLength(FinalPath, 32768);
    Len := GetFinalPathNameByHandle(Handle, FinalPath, 32768, 0);
    if (Len = 0) or (Len >= 32768) then begin
      ReportDataDeletionFailure('Could not delete application data. Failed to resolve canonical path for:' + #13#10 + NormalizedPath);
      Exit;
    end;
    SetLength(FinalPath, Len);
  finally
    CloseHandle(Handle);
  end;
  if Copy(FinalPath, 1, 4) <> '\\?\' then begin
    ReportDataDeletionFailure('Application data was not deleted because the resolved directory is not a local path:' + #13#10 + NormalizedPath);
    Exit;
  end;
  FinalPath := Copy(FinalPath, 5, Length(FinalPath));
  if not ValidateSafeLocalPath(FinalPath, NormalizedPath, 'the resolved directory', NormalizedFinal) then
    Exit;
  SetLength(LongPath, 32768);
  Len := GetLongPathName(NormalizedPath, LongPath, 32768);
  if (Len = 0) or (Len >= 32768) then begin
    ReportDataDeletionFailure('Could not delete application data. Failed to resolve full path for:' + #13#10 + NormalizedPath);
    Exit;
  end;
  SetLength(LongPath, Len);
  if CompareText(StripTrailingBackslashes(LongPath), NormalizedFinal) <> 0 then begin
    ReportDataDeletionFailure('Application data was not deleted because the path traverses a symbolic link or junction:' + #13#10 + NormalizedPath);
    Exit;
  end;
  Deleted := DelTree(NormalizedPath, True, True, True);
  if (not Deleted) or DirExists(NormalizedPath) then begin
    ReportDataDeletionFailure('Could not completely delete application data. Files may remain at:' + #13#10 + NormalizedPath);
    Exit;
  end;
  Log('Deleted application data directory: ' + NormalizedPath);
  if DirExists(ExpandConstant('{app}')) then
    RemoveDir(ExpandConstant('{app}'));
end;

function InitializeUninstall: Boolean;
begin
  UninstallDataDir := ResolveUninstallDataDir;
  DeleteApplicationDataRequested := HasDeleteAppDataParam;
  Result := True;
end;

procedure InitializeUninstallProgressForm;
begin
  if not UninstallSilent then begin
    if not PromptDeleteApplicationData(UninstallDataDir, DeleteApplicationDataRequested) then
      Abort;
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
begin
  if CurUninstallStep = usUninstall then begin
    StopTray;
    ChangeServiceState(ServiceStopped);
    RunSC('delete ' + ServiceName, True);
  end else if CurUninstallStep = usPostUninstall then begin
    if DeleteApplicationDataRequested then
      DeleteVerifiedApplicationData(UninstallDataDir);
  end;
end;
