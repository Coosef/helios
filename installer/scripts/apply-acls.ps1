<#
.SYNOPSIS
  S1-T29 — harden the ProgramData\BeyzBackup tree ACLs at install time.

.DESCRIPTION
  The NTFS ACL is the real confidentiality boundary against a live local
  attacker (DPAPI only protects the stolen-disk case — Technical Design Section
  0.4), so the ACLs are MANDATORY and set at folder-create time, fail-closed: if
  any grant cannot be applied the script exits non-zero and the installer aborts
  BEFORE any credential (config / one-shot enrollment token) is written.

  Locale-independent: every principal is a well-known SID, never a localized
  name ("BUILTIN\Administrators" differs per UI language).

    S-1-5-18      NT AUTHORITY\SYSTEM
    S-1-5-32-544  BUILTIN\Administrators
    S-1-5-32-545  BUILTIN\Users

  Matrix (inheritance broken on every node so nothing leaks in from the parent):

    <Root>        SYSTEM + Administrators  Full          (Users have NO access)
    state\        SYSTEM + Administrators  Full          (Users removed)
    update\       SYSTEM + Administrators  Full          (Users removed)
    logs\         SYSTEM/Admin Full, Users Read+Execute
    config.yaml   SYSTEM/Admin Full, Users Read

  The one-shot enrollment token file (state\enroll-token) is locked to SYSTEM
  ONLY (Administrators excluded); that tighter grant is applied by the installer
  immediately after the token is written, not here.

.PARAMETER Root
  The ProgramData root, e.g. C:\ProgramData\BeyzBackup.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string] $Root
)

$ErrorActionPreference = 'Stop'

$SID_SYSTEM = '*S-1-5-18'
$SID_ADMINS = '*S-1-5-32-544'
$SID_USERS  = '*S-1-5-32-545'

# Run an icacls invocation and fail closed on a non-zero exit.
function Invoke-Icacls {
    param([Parameter(Mandatory = $true)][string[]] $IcaclsArgs, [string] $What)
    & icacls.exe @IcaclsArgs | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Error "icacls failed ($What): exit $LASTEXITCODE"
        exit 1
    }
}

$state  = Join-Path $Root 'state'
$logs   = Join-Path $Root 'logs'
$update = Join-Path $Root 'update'
$config = Join-Path $Root 'config.yaml'

foreach ($d in @($Root, $state, $logs, $update)) {
    if (-not (Test-Path -LiteralPath $d)) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
}

# Root: break inheritance, SYSTEM + Administrators Full (subtree). No Users.
Invoke-Icacls @($Root, '/inheritance:r',
    '/grant:r', "${SID_SYSTEM}:(OI)(CI)F",
    '/grant:r', "${SID_ADMINS}:(OI)(CI)F") 'root'

# state\ and update\: SYSTEM + Administrators ONLY (Users removed).
foreach ($d in @($state, $update)) {
    Invoke-Icacls @($d, '/inheritance:r',
        '/grant:r', "${SID_SYSTEM}:(OI)(CI)F",
        '/grant:r', "${SID_ADMINS}:(OI)(CI)F") $d
}

# logs\: SYSTEM/Admin Full, Users Read+Execute (read-only).
Invoke-Icacls @($logs, '/inheritance:r',
    '/grant:r', "${SID_SYSTEM}:(OI)(CI)F",
    '/grant:r', "${SID_ADMINS}:(OI)(CI)F",
    '/grant:r', "${SID_USERS}:(OI)(CI)RX") 'logs'

# config.yaml: SYSTEM/Admin Full, Users Read (non-secret operator settings).
if (Test-Path -LiteralPath $config) {
    Invoke-Icacls @($config, '/inheritance:r',
        '/grant:r', "${SID_SYSTEM}:F",
        '/grant:r', "${SID_ADMINS}:F",
        '/grant:r', "${SID_USERS}:R") 'config.yaml'
}

# Fail-closed verification: state\ must NOT be reachable by Users / Everyone.
$acl = (& icacls.exe $state) -join "`n"
if ($acl -match 'S-1-5-32-545' -or $acl -match 'S-1-1-0' -or $acl -match 'Everyone' -or $acl -match '\\Users:') {
    Write-Error "state ACL still grants Users/Everyone after hardening; aborting"
    exit 1
}

Write-Output "ACL hardening complete for $Root"
exit 0
