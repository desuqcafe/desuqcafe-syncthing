; Inno Setup script for desuqcafe Syncthing.
;
; Built by custom/build-windows.ps1, which passes every /D value below. The
; defaults exist only so the script can also be opened directly in the Inno
; Setup IDE.
;
; This is a PER-USER install: it goes under %LOCALAPPDATA% and needs no
; administrator rights, so the person installing it never sees a UAC prompt.

#ifndef MyAppName
  #define MyAppName        "desuqcafe Syncthing"
#endif
#ifndef MyAppBinary
  #define MyAppBinary      "desuq-syncthing"
#endif
#ifndef MyAppPublisher
  #define MyAppPublisher   "desuqcafe"
#endif
#ifndef MyAppVersion
  #define MyAppVersion     "0.0.0"
#endif
#ifndef MyAppVersionFull
  #define MyAppVersionFull "dev"
#endif
#ifndef MyAppUrl
  #define MyAppUrl         "https://github.com/desuqcafe/desuqcafe-syncthing"
#endif
#ifndef MyAppId
  #define MyAppId          "33BDCF88-0798-4D57-906E-CE272E9E3AD3"
#endif
#ifndef MyDataDir
  #define MyDataDir        "desuqcafe-syncthing"
#endif

[Setup]
; "{{" is an escaped literal brace, so this resolves to {GUID}.
AppId={{{#MyAppId}}
AppName={#MyAppName}
AppVersion={#MyAppVersionFull}
VersionInfoVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersionFull}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppUrl}
AppSupportURL={#MyAppUrl}/issues
AppUpdatesURL={#MyAppUrl}/releases

; Per-user install: no administrator rights, no UAC prompt.
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
DefaultDirName={localappdata}\Programs\{#MyAppBinary}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
DisableDirPage=auto

OutputDir=..\dist
OutputBaseFilename={#MyAppBinary}-setup-{#MyAppVersion}
SetupIconFile=..\..\assets\logo.ico
UninstallDisplayIcon={app}\{#MyAppBinary}.exe
UninstallDisplayName={#MyAppName}

Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
CloseApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "startupicon"; Description: "Start {#MyAppName} automatically when I sign in"; GroupDescription: "Startup:"
Name: "desktopicon"; Description: "Create a desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked

[Files]
Source: "..\dist\{#MyAppBinary}.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
; Double-clicking starts the daemon and opens the web GUI. If it is already
; running, Syncthing detects the existing instance and just opens the GUI.
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}.exe"; \
    Parameters: "serve --home=""{localappdata}\{#MyDataDir}"""; \
    WorkingDir: "{app}"; Comment: "Start {#MyAppName} and open the web interface"

Name: "{group}\{#MyAppName} data folder"; Filename: "{localappdata}\{#MyDataDir}"

Name: "{userdesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}.exe"; \
    Parameters: "serve --home=""{localappdata}\{#MyDataDir}"""; \
    WorkingDir: "{app}"; Tasks: desktopicon

; Autostart shortcut: same thing, minus opening a browser window at sign-in.
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}.exe"; \
    Parameters: "serve --home=""{localappdata}\{#MyDataDir}"" --no-browser"; \
    WorkingDir: "{app}"; Tasks: startupicon

[Run]
Filename: "{app}\{#MyAppBinary}.exe"; \
    Parameters: "serve --home=""{localappdata}\{#MyDataDir}"""; \
    WorkingDir: "{app}"; Description: "Start {#MyAppName} now"; \
    Flags: nowait postinstall skipifsilent

[Code]
// Stop a running instance before installing over it or uninstalling, so the
// executable is not locked and the database is closed cleanly.
procedure StopRunningInstance();
var
  ResultCode: Integer;
begin
  // Ask politely first, then insist.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/IM {#MyAppBinary}.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(1500);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppBinary}.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(500);
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  StopRunningInstance();
  Result := '';
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  DataDir: String;
begin
  if CurUninstallStep = usUninstall then
    StopRunningInstance();

  if CurUninstallStep = usPostUninstall then
  begin
    DataDir := ExpandConstant('{localappdata}\{#MyDataDir}');
    if DirExists(DataDir) then
    begin
      // Keeping the folder preserves this machine's device identity and folder
      // setup, so default to No.
      if SuppressibleMsgBox(
           'Also delete your {#MyAppName} configuration and database?' #13#10 #13#10
           + DataDir + #13#10 #13#10
           + 'This removes this device''s identity and folder settings. '
           + 'Your synchronised files themselves are NOT touched.' #13#10 #13#10
           + 'Choose No if you plan to reinstall.',
           mbConfirmation, MB_YESNO or MB_DEFBUTTON2, IDNO) = IDYES then
        DelTree(DataDir, True, True, True);
    end;
  end;
end;
