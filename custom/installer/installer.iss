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
  #define MyAppVersion     "0.0.0.0"
#endif
#ifndef MyAppVersionFull
  #define MyAppVersionFull "dev"
#endif
; Base name of the installer itself. Named after the tag rather than the
; numeric version, so two releases off the same upstream base -- desuq.1 and
; desuq.2, whose x.y.z are identical -- do not publish two different files
; under one name.
#ifndef MyAppSetupName
  #define MyAppSetupName   MyAppBinary + "-setup-" + MyAppVersionFull
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
OutputBaseFilename={#MyAppSetupName}
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
Source: "..\dist\{#MyAppBinary}-tray.exe"; DestDir: "{app}"; Flags: ignoreversion
; Shipped rather than run from a temp dir so it can be re-run by hand later,
; e.g. with -Force after the recommended defaults change.
Source: "..\scripts\seed-config.ps1"; DestDir: "{app}"; Flags: ignoreversion

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

; Autostart runs the tray rather than the daemon directly. The tray starts
; Syncthing, keeps it running, and is the only thing on screen that says whether
; it is working -- Syncthing itself has no tray icon and no service mode, so
; started bare at sign-in it is completely invisible.
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -binary=""{app}\{#MyAppBinary}.exe"""; \
    WorkingDir: "{app}"; Tasks: startupicon; \
    Comment: "Run {#MyAppName} in the notification area"

[Run]
; Seed config.xml with the defaults this team needs -- staggered versioning, a
; 20 GB disk reserve, the Blender ignore set -- before Syncthing ever starts,
; so the modellers get a working setup without touching Settings. Entries
; without the postinstall flag run during installation, so this is guaranteed
; to complete before the "Start now" entry below.
;
; The script no-ops on an upgrade unless the seeded defaults have changed, and
; swallows its own errors, so a failure here can never block the install.
Filename: "{sys}\WindowsPowerShell\v1.0\powershell.exe"; \
    Parameters: "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File ""{app}\seed-config.ps1"" -DataDir ""{localappdata}\{#MyDataDir}"" -Binary ""{app}\{#MyAppBinary}.exe"""; \
    WorkingDir: "{app}"; StatusMsg: "Preparing your {#MyAppName} settings..."; \
    Flags: runhidden waituntilterminated

; Start the tray rather than the daemon directly, so what the user gets now is
; the same thing they will get at sign-in. -open makes it show the web
; interface once it is up, which the sign-in shortcut deliberately does not do.
Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -binary=""{app}\{#MyAppBinary}.exe"" -open"; \
    WorkingDir: "{app}"; Description: "Start {#MyAppName} now"; \
    Flags: nowait postinstall skipifsilent

[Code]
// Stop a running instance before installing over it or uninstalling, so the
// executable is not locked and the database is closed cleanly.
procedure StopRunningInstance();
var
  ResultCode: Integer;
begin
  // The tray first: it supervises Syncthing and would restart it underneath us.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppBinary}-tray.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(500);

  // Then the daemon: ask politely first, then insist.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/IM {#MyAppBinary}.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(1500);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppBinary}.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(500);
end;

// Take the violet folder icons off every synced folder before the files that
// make them work are deleted.
//
// desktop.ini names {app}\folder.ico by absolute path, so after an uninstall
// every marker points at a file that is gone. Explorer falls back to the plain
// icon silently, which makes this litter rather than breakage -- but it is
// litter in the user's own project folders, and it also leaves them carrying
// the read-only attribute that says "customised".
//
// This has to run at usUninstall, not usPostUninstall: it needs the tray
// binary, which is still on disk at that point, and config.xml, which holds
// the folder list and is only removed later (and only if the user says yes).
// It is right either way -- if they keep the data directory to reinstall
// later, the tray re-marks every folder on its next reconcile.
procedure ClearFolderIcons();
var
  Tray: String;
  ResultCode: Integer;
begin
  Tray := ExpandConstant('{app}\{#MyAppBinary}-tray.exe');
  if not FileExists(Tray) then
    Exit;
  // Failure here is never a reason to block an uninstall, so the result is
  // deliberately not checked.
  Exec(Tray,
       ExpandConstant('--home="{localappdata}\{#MyDataDir}" --clear-folder-icons'),
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
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
  begin
    StopRunningInstance();
    ClearFolderIcons();
  end;

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
