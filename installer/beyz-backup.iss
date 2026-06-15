; ============================================================================
; Beyz Backup Agent - Windows Installer  (Sprint 1 / S1-T29)
; ----------------------------------------------------------------------------
; Inno Setup 6 script. Beyz System A.S.
;
; UNSIGNED: Authenticode signing is S1-T30. This script is "signing-ready" - the
; SignTool hook below only needs to be enabled (no structural change) once T30
; provides a signing key. Update trust still relies on the Ed25519 manifest
; signature, NOT on Authenticode (Technical Design Section 0.5).
;
; What it does (matches the agent/updater compiled path defaults exactly):
;   * installs BOTH binaries to  C:\Program Files\BeyzBackup\
;   * builds the ACL-locked tree  C:\ProgramData\BeyzBackup\{config.yaml,state,logs,update}
;   * writes the single-use enrollment token to  state\enroll-token  (SYSTEM-only),
;     never to config.yaml and never to any log
;   * registers + starts the SINGLE  BeyzBackupAgent  service (LocalSystem, auto)
;     via the agent's own control verbs
;   * the updater binary is INSTALLED but NEVER registered as a service (Section
;     0.6 / AC-42) - there is no second persistent service
;
; Local build is Windows-only (ISCC.exe). On non-Windows hosts the .iss content
; is validated by the Go static tests in installer/installer_test.go; the setup
; artifact itself is produced by the non-blocking Windows CI job.
; ============================================================================

#define AppName        "Beyz Backup Agent"
#define AppPublisher   "Beyz System A.S."
#define AppVersion     "0.1.0"
#define AgentExe       "beyz-backup-agent.exe"
#define UpdaterExe     "beyz-backup-updater.exe"
#define ServiceName    "BeyzBackupAgent"

; Directory holding the cross-compiled windows/amd64 binaries. Overridable:
;   ISCC.exe /DBinDir=..\dist\windows installer\beyz-backup.iss
#ifndef BinDir
  #define BinDir "..\dist\windows"
#endif

[Setup]
; Stable upgrade identity - NEVER change this GUID (upgrades key off it).
AppId={{8F2A7C3E-1B4D-4E6A-9C2F-7A1E3D5B9F04}
AppName={#AppName}
AppVersion={#AppVersion}
AppPublisher={#AppPublisher}
DefaultDirName={commonpf}\BeyzBackup
DisableDirPage=yes
DisableProgramGroupPage=yes
UninstallDisplayName={#AppName}
OutputBaseFilename=BeyzBackupSetup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
; The installer touches HKLM, Program Files, ProgramData and the SCM -> admin.
PrivilegesRequired=admin
ArchitecturesAllowed=x64
ArchitecturesInstallIn64BitMode=x64
VersionInfoVersion={#AppVersion}
VersionInfoCompany={#AppPublisher}
VersionInfoProductName={#AppName}
; --- signing-ready (S1-T30) -------------------------------------------------
; Provide a sign tool to ISCC (  /Ssigntool="signtool sign /fd sha256 $f"  )
; and uncomment the next line. T30 owns this; the installer ships UNSIGNED now.
; SignTool=signtool

[Files]
; Both binaries land in Program Files; the updater is NOT registered as a service.
Source: "{#BinDir}\{#AgentExe}";   DestDir: "{app}"; Flags: ignoreversion
Source: "{#BinDir}\{#UpdaterExe}"; DestDir: "{app}"; Flags: ignoreversion
; ACL hardening helper (invoked post-install).
Source: "scripts\apply-acls.ps1"; DestDir: "{app}\scripts"; Flags: ignoreversion
; config.yaml: laid down ONCE from the repo sample; PRESERVED on upgrade
; (onlyifdoesntexist) and never auto-removed (uninstall is handled in [Code]).
Source: "..\configs\config.sample.yaml"; DestDir: "{commonappdata}\BeyzBackup"; \
    DestName: "config.yaml"; Flags: onlyifdoesntexist uninsneveruninstall

[Dirs]
; ProgramData tree. Precise ACLs are applied post-install by apply-acls.ps1
; (Inno's built-in Permissions cannot break inheritance the way Section 0.4 requires).
Name: "{commonappdata}\BeyzBackup"
Name: "{commonappdata}\BeyzBackup\state"
Name: "{commonappdata}\BeyzBackup\logs"
Name: "{commonappdata}\BeyzBackup\update"

[Run]
; Service lifecycle via the agent's OWN verbs (kardianos-registered, LocalSystem).
; The token + ACLs are written earlier in [Code] ssPostInstall, never here (so the
; token can never reach an Inno [Run] log line).
Filename: "{app}\{#AgentExe}"; Parameters: "install --config ""{commonappdata}\BeyzBackup\config.yaml"""; \
    Flags: runhidden waituntilterminated; StatusMsg: "Registering {#ServiceName} service..."
Filename: "{sys}\sc.exe"; Parameters: "config {#ServiceName} start= auto"; \
    Flags: runhidden waituntilterminated; StatusMsg: "Configuring automatic start..."
Filename: "{sys}\sc.exe"; Parameters: "failure {#ServiceName} reset= 86400 actions= restart/60000/restart/60000/restart/120000"; \
    Flags: runhidden waituntilterminated; StatusMsg: "Configuring failure recovery..."
Filename: "{app}\{#AgentExe}"; Parameters: "start"; \
    Flags: runhidden waituntilterminated; StatusMsg: "Starting {#ServiceName}..."

[UninstallRun]
; Stop + remove the single agent service, then defensively remove any updater
; scheduled task (S1-T29 does NOT create one - see docs; this is a no-op safety net).
Filename: "{app}\{#AgentExe}"; Parameters: "stop"; \
    Flags: runhidden waituntilterminated; RunOnceId: "BeyzStopAgent"
Filename: "{app}\{#AgentExe}"; Parameters: "uninstall"; \
    Flags: runhidden waituntilterminated; RunOnceId: "BeyzRemoveAgent"
Filename: "{sys}\schtasks.exe"; Parameters: "/delete /tn ""BeyzBackupUpdater"" /f"; \
    Flags: runhidden runascurrentuser; RunOnceId: "BeyzRemoveUpdaterTask"

[Code]
var
  TokenPage: TInputQueryWizardPage;

const
  DataRootRel = '\BeyzBackup';

// ---- anti-rollback (Section 0.7 / REV-4): refuse to install OVER a newer build ----
function InitializeSetup(): Boolean;
var
  installedStr: String;
  installed, current: Int64;
begin
  Result := True;
  if RegQueryStringValue(HKLM,
      'SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\{8F2A7C3E-1B4D-4E6A-9C2F-7A1E3D5B9F04}_is1',
      'DisplayVersion', installedStr) then
  begin
    if StrToVersion(installedStr, installed) and StrToVersion('{#AppVersion}', current) then
    begin
      if installed > current then
      begin
        MsgBox('A newer version (' + installedStr + ') of ' + '{#AppName}' +
               ' is already installed. Downgrade is not supported.', mbError, MB_OK);
        Result := False;
      end;
    end;
  end;
end;

// ---- interactive masked token page (skipped on silent installs) ----
procedure InitializeWizard();
begin
  TokenPage := CreateInputQueryPage(wpWelcome,
    'Enrollment Token',
    'Provide the single-use device enrollment token',
    'Paste the enrollment token issued by your Beyz Backup console. It is written' + #13#10 +
    'only to the ACL-protected state store (state\enroll-token) and is NEVER stored' + #13#10 +
    'in config.yaml or written to any log. Leave blank for silent installs that pass' + #13#10 +
    '/TOKENFILE=<path> (preferred) or /TOKEN=<value>.');
  TokenPage.Add('Enrollment token:', True); { True => masked input }
end;

// ---- token source: env-style precedence handled by the AGENT; the INSTALLER
//      source order is /TOKENFILE= (log-safe) > /TOKEN= > masked wizard. ----
function ResolveToken(): String;
var
  tokenFile, raw: String;
begin
  Result := '';
  tokenFile := ExpandConstant('{param:TOKENFILE|}');
  if tokenFile <> '' then
  begin
    if LoadStringFromFile(tokenFile, raw) then
      Result := Trim(raw);
    Exit;
  end;
  raw := ExpandConstant('{param:TOKEN|}');
  if raw <> '' then
  begin
    Result := Trim(raw);
    Exit;
  end;
  if TokenPage <> nil then
    Result := Trim(TokenPage.Values[0]);
end;

function DataRoot(): String;
begin
  Result := ExpandConstant('{commonappdata}') + DataRootRel;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  resExit: Integer;
  tok, tokenPath, aclScript: String;
begin
  if CurStep <> ssPostInstall then
    Exit;

  // 1. Harden the ProgramData ACLs - FAIL CLOSED before any credential is written.
  aclScript := ExpandConstant('{app}\scripts\apply-acls.ps1');
  if (not Exec(ExpandConstant('{sys}\WindowsPowerShell\v1.0\powershell.exe'),
        '-NoProfile -ExecutionPolicy Bypass -File "' + aclScript + '" -Root "' + DataRoot() + '"',
        '', SW_HIDE, ewWaitUntilTerminated, resExit)) or (resExit <> 0) then
  begin
    MsgBox('Failed to secure the BeyzBackup data-directory ACLs. Installation aborted' + #13#10 +
           'before any credential was written.', mbError, MB_OK);
    Abort();
  end;

  // 2. Write the one-shot enrollment token via SaveStringToFile (raw bytes, trimmed
  //    by the agent on read) - NEVER on a command line, so it cannot reach a log.
  tok := ResolveToken();
  if tok <> '' then
  begin
    tokenPath := DataRoot() + '\state\enroll-token';
    if not SaveStringToFile(tokenPath, tok, False) then
    begin
      MsgBox('Failed to write the enrollment token file.', mbError, MB_OK);
      Abort();
    end;
    // 3. Lock the token file to SYSTEM ONLY (Administrators excluded) - FAIL CLOSED.
    //    A bearer token must never linger under the weaker inherited (SYSTEM +
    //    Administrators) ACL, so if icacls cannot apply the SYSTEM-only grant we
    //    DELETE the token and abort rather than leave it readable by Administrators.
    if (not Exec(ExpandConstant('{sys}\icacls.exe'),
          '"' + tokenPath + '" /inheritance:r /grant:r *S-1-5-18:F',
          '', SW_HIDE, ewWaitUntilTerminated, resExit)) or (resExit <> 0) then
    begin
      DeleteFile(tokenPath);
      MsgBox('Failed to restrict the enrollment token file to SYSTEM only. The token' + #13#10 +
             'was removed; re-run the installer (preferably with /TOKENFILE=).', mbError, MB_OK);
      Abort();
    end;
  end;
end;

// On upgrade the running service holds a handle to the agent .exe; stop it BEFORE
// the new binaries are copied. Best-effort (a fresh install has nothing to stop).
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  resExit: Integer;
begin
  Result := '';
  Exec(ExpandConstant('{sys}\sc.exe'), 'stop {#ServiceName}', '',
       SW_HIDE, ewWaitUntilTerminated, resExit);
end;

// ---- uninstall: default shreds state; /KEEPSTATE preserves identity ----
function UninstallKeepState(): Boolean;
begin
  Result := (ExpandConstant('{param:KEEPSTATE|0}') <> '0');
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  root: String;
begin
  if CurUninstallStep <> usPostUninstall then
    Exit;
  root := DataRoot();
  if UninstallKeepState() then
  begin
    // Preserve state\ (device_guid + cert + key) AND config.yaml for device
    // replacement / reinstall; remove the volatile dirs only.
    DelTree(root + '\logs', True, True, True);
    DelTree(root + '\update', True, True, True);
  end
  else
  begin
    // Default: crypto-shred by removal. The state secrets are DPAPI machine-scope
    // wrapped, so deleting them renders them unrecoverable without the host key.
    DelTree(root, True, True, True);
  end;
end;
