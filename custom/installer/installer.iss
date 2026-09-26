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
; The optional Blender add-on (custom/blender). Shipped, not installed: putting
; it into Blender means writing into Blender's own settings, per Blender
; version, and that is the modeller's choice to make -- Edit > Preferences >
; Add-ons > Install from Disk. The main screen says where it is.
Source: "..\dist\desuq_syncthing.zip"; DestDir: "{app}\blender"; Flags: ignoreversion

[Icons]
; EVERY way in starts the tray, never the daemon directly.
;
; These two used to run `{#MyAppBinary}.exe serve`, which starts Syncthing with
; no notification-area icon at all -- so searching Windows for the app and
; pressing Enter gave you a browser tab and nothing else, while the sign-in
; shortcut gave you an icon. Same product, two different behaviours depending
; on how you happened to launch it, and the one most people find by searching
; was the one with no way to see whether it was still running.
;
; It was also silently unrepeatable: `serve` notices an existing instance and
; exits 0, so the second click did nothing whatsoever, not even open the GUI.
;
; -open shows the web interface once Syncthing is up, matching the post-install
; button below. The sign-in shortcut deliberately does not. The tray refuses to
; start a second copy of itself for the same home and just opens the GUI
; instead (see instance_windows.go), so clicking these while it is already
; running does the obvious thing rather than putting two icons in the tray.
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -binary=""{app}\{#MyAppBinary}.exe"" -open"; \
    WorkingDir: "{app}"; Comment: "Start {#MyAppName} and open the web interface"

Name: "{group}\{#MyAppName} data folder"; Filename: "{localappdata}\{#MyDataDir}"

Name: "{userdesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -binary=""{app}\{#MyAppBinary}.exe"" -open"; \
    WorkingDir: "{app}"; Tasks: desktopicon; \
    Comment: "Start {#MyAppName} and open the web interface"

; The same tray as the shortcuts above, minus -open: signing in should not
; throw a browser tab at you. The tray starts Syncthing, keeps it running, and
; is the only thing on screen that says whether it is working -- Syncthing
; itself has no tray icon and no service mode, so started bare at sign-in it is
; completely invisible.
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -binary=""{app}\{#MyAppBinary}.exe"""; \
    WorkingDir: "{app}"; Tasks: startupicon; \
    Comment: "Run {#MyAppName} in the notification area"

; "I'm working on this" -- right-click a file in Explorer, Send to. The claim
; is made from where the file is, because that is where a modeller is when they
; decide to open it; a browser cannot see the file at all. Sending the same
; file again takes the mark off, and the toast says which way it went. See
; custom/tray/claims.go.
;
; A Send To entry is an ordinary shortcut in a per-user folder, not a shell
; extension: no DLL is loaded into Explorer, which given the tray's history
; with Defender (DEPLOYMENT-3D-TEAM.md section 20) is the point. Explorer
; appends the selected files after the parameters below.
Name: "{usersendto}\{#MyAppName} - I'm working on this"; Filename: "{app}\{#MyAppBinary}-tray.exe"; \
    Parameters: "-home=""{localappdata}\{#MyDataDir}"" -claim"; \
    WorkingDir: "{app}"; \
    Comment: "Tell everyone you share this file with that you are working on it"

[Registry]
; Right-click a .blend in Explorer: a "desuqcafe Syncthing" submenu with I'm
; working on this, Show history, Who has this? and Say why I changed this. See
; custom/tray/explorer.go, custom/tray/notes.go and DEPLOYMENT-3D-TEAM.md
; sections 27 and 31.
;
; Static verbs under SystemFileAssociations, per user: every one is a command
; line that runs the tray with the file's path, and nothing is loaded into
; Explorer -- the same reasoning as the Send To entry above, and the reason
; overlay icons and property handlers were turned down. SystemFileAssociations
; rather than the .blend ProgID, so Blender's own association is not touched
; and it does not matter whether Blender is installed yet.
;
; On Windows 11 these appear under "Show more options", like Send To. The
; compact menu only shows verbs from a packaged app's IExplorerCommand, which
; is a COM server in Explorer by another name.
;
; SubCommands="" plus a nested shell key is how a static verb becomes a
; cascading menu without a DLL. The numeric prefixes fix the order, which is
; otherwise alphabetical by key name.
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe"; \
    ValueType: string; ValueName: "MUIVerb"; ValueData: "{#MyAppName}"; Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe"; \
    ValueType: string; ValueName: "SubCommands"; ValueData: ""
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe"; \
    ValueType: string; ValueName: "Icon"; ValueData: """{app}\{#MyAppBinary}-tray.exe"",0"
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\1claim"; \
    ValueType: string; ValueName: "MUIVerb"; ValueData: "I'm working on this (or done with it)"
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\1claim\command"; \
    ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppBinary}-tray.exe"" -home=""{localappdata}\{#MyDataDir}"" -claim ""%1"""
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\2history"; \
    ValueType: string; ValueName: "MUIVerb"; ValueData: "Show history"
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\2history\command"; \
    ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppBinary}-tray.exe"" -home=""{localappdata}\{#MyDataDir}"" -history ""%1"""
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\3who"; \
    ValueType: string; ValueName: "MUIVerb"; ValueData: "Who has this?"
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\3who\command"; \
    ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppBinary}-tray.exe"" -home=""{localappdata}\{#MyDataDir}"" -who ""%1"""
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\4note"; \
    ValueType: string; ValueName: "MUIVerb"; ValueData: "Say why I changed this"
Root: HKCU; Subkey: "Software\Classes\SystemFileAssociations\.blend\shell\desuqcafe\shell\4note\command"; \
    ValueType: string; ValueName: ""; \
    ValueData: """{app}\{#MyAppBinary}-tray.exe"" -home=""{localappdata}\{#MyDataDir}"" -note ""%1"""

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
  Tray: String;
  ResultCode: Integer;
begin
  // The tray first: it supervises Syncthing and would restart it underneath us.
  // Killing it outright is fine -- it holds no state of its own.
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM {#MyAppBinary}-tray.exe',
       '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(500);

  // Then the daemon, which does hold state: an open database.
  //
  // The obvious "ask politely, then insist" -- taskkill without /F, then with
  // -- does not work here at all. Without /F taskkill delivers WM_CLOSE to the
  // process's top-level windows, and this binary is linked -H windowsgui with
  // no window to deliver to, so the polite call is a guaranteed no-op and
  // every upgrade in fact ended in the hard kill. Syncthing survives that, but
  // it is the path that leaves the database needing recovery on next start,
  // which on a texture library is minutes of rescanning the user did not ask
  // for.
  //
  // So ask over the REST API instead, which is what the tray's own Quit does.
  // Reusing the tray binary as a one-shot rather than reimplementing it here
  // keeps the API key and the GUI address being read out of config.xml in one
  // place -- and in Pascal, over a self-signed https GUI, it would be a good
  // deal more than one place.
  //
  // {app} still holds the OLD tray at this point, so on an upgrade from a
  // build that predates -shutdown the flag is rejected and the exit code is
  // non-zero. That is not worth branching on: the force-kill below is the same
  // backstop it has always been, and a fresh install has nothing running.
  Tray := ExpandConstant('{app}\{#MyAppBinary}-tray.exe');
  if FileExists(Tray) then
  begin
    Exec(Tray,
         ExpandConstant('-home="{localappdata}\{#MyDataDir}" -shutdown'),
         '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  end
  else
    Sleep(1500);

  // Whatever happened above, make sure nothing is left holding the exe.
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

// Check that Windows Security has not taken the tray.
//
// Defender has quarantined {#MyAppBinary}-tray.exe on install as
// Trojan:Win32/Bearfoos.A!ml, a machine-learning false positive, within
// seconds of setup finishing -- and both shortcuts with it. The sign-in
// shortcut is what starts Syncthing, so the machine then syncs normally until
// its next restart and silently never again (DEPLOYMENT-3D-TEAM.md section
// 20). Nothing afterwards can say so: the tray is the thing that was removed.
// Setup is still here, and is the one moment the person is looking.
//
// Only the file is checked. Quarantine removes it, and that is the fact that
// matters; whether the tray is *running* is a separate question with benign
// answers ("Start now" unticked).
//
// This deliberately stops at telling. Restoring from quarantine and adding
// an exclusion are security decisions, and they are made in Windows
// Security's own window, by the person, not by an installer.
procedure WarnIfTrayRemoved();
var
  Tray: String;
  Waited, ResultCode: Integer;
begin
  Tray := ExpandConstant('{app}\{#MyAppBinary}-tray.exe');

  // ssDone comes after the post-install "Start now" entry, so the tray has
  // been started by now -- which is when a behavioural classifier looks. Up
  // to twenty seconds covers "within seconds" with room; a healthy install
  // waits the full time, which is why the window is hidden first: Sleep does
  // not pump messages, and a visible window would read as a hang.
  Log('Checking Windows Security has left the tray in place: ' + Tray);
  WizardForm.Hide;
  Waited := 0;
  while FileExists(Tray) and (Waited < 20000) do
  begin
    Sleep(500);
    Waited := Waited + 500;
  end;
  if FileExists(Tray) then
    Exit;

  Log('Tray missing after install: ' + Tray);
  if SuppressibleMsgBox(
       'Windows Security has removed part of {#MyAppName}.' #13#10 #13#10
       + 'It is a false alarm, but it matters: without that part, syncing will '
       + 'stop the next time this computer restarts, and nothing will say so.' #13#10 #13#10
       + 'To put it back: in Windows Security, open Virus & threat protection, '
       + 'then Protection history. Find the entry that mentions '
       + '{#MyAppBinary}-tray and choose Restore.' #13#10 #13#10
       + 'If you are not sure, ask whoever sent you this installer before you '
       + 'restart.' #13#10 #13#10
       + 'Open Windows Security now?',
       mbError, MB_YESNO, IDNO) = IDYES then
    ShellExec('', 'windowsdefender://threat', '', '', SW_SHOWNORMAL, ewNoWait, ResultCode);
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  // A silent install is somebody scripting it, who does not want a twenty-
  // second tail on every run; the main screen carries the same warning.
  if (CurStep = ssDone) and not WizardSilent then
    WarnIfTrayRemoved();
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
