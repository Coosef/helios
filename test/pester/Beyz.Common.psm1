<#
.SYNOPSIS
  S1-T35 shared helpers for the Windows installer / service Pester suites.

  Locale-independent (well-known SIDs, never localized names), self-cleaning,
  and bounded (every SCM wait has a deadline - no fixed Start-Sleep). Nothing
  here talks to a real SaaS; the agent is expected to FAIL to fully boot on the
  CI runner (no compiled SPKI pins / no reachable control plane), so the suites
  assert the deterministic plumbing (registration, ACLs, lifecycle, uninstall),
  not "service reaches RUNNING" (that is the installer-with-pins domain).
#>

Set-StrictMode -Version Latest

# Well-known SIDs (locale-independent).
$script:SID_SYSTEM = 'S-1-5-18'
$script:SID_ADMINS = 'S-1-5-32-544'
$script:SID_USERS  = 'S-1-5-32-545'
$script:SID_EVERYONE = 'S-1-1-0'

function Get-BeyzRepoRoot {
    # test/pester/ -> repo root is two levels up.
    return (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
}

# Resolve the cross-compiled windows binaries dir (override: $env:BEYZ_BIN_DIR).
function Get-BeyzBinDir {
    if ($env:BEYZ_BIN_DIR) { return $env:BEYZ_BIN_DIR }
    return (Join-Path (Get-BeyzRepoRoot) 'dist\windows')
}

# Resolve the built setup.exe (override: $env:BEYZ_SETUP_EXE).
function Get-BeyzSetupExe {
    if ($env:BEYZ_SETUP_EXE -and (Test-Path -LiteralPath $env:BEYZ_SETUP_EXE)) { return $env:BEYZ_SETUP_EXE }
    $out = Join-Path (Get-BeyzRepoRoot) 'installer\Output'
    if (Test-Path -LiteralPath $out) {
        $exe = Get-ChildItem -LiteralPath $out -Filter 'BeyzBackupSetup-*.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($exe) { return $exe.FullName }
    }
    return $null
}

# True when the named Windows service is registered (sc query 1060 == absent).
function Test-BeyzServiceExists {
    param([Parameter(Mandatory)][string]$Name)
    & sc.exe query $Name *> $null
    return ($LASTEXITCODE -ne 1060)
}

# SERVICE_START_NAME (e.g. LocalSystem) from `sc qc`, locale-independent token.
function Get-BeyzServiceStartName {
    param([Parameter(Mandatory)][string]$Name)
    $out = (& sc.exe qc $Name) -join "`n"
    foreach ($line in ($out -split "`n")) {
        if ($line -match 'SERVICE_START_NAME\s*:\s*(.+?)\s*$') { return $Matches[1].Trim() }
    }
    return $null
}

# Bounded wait until the service reaches $Status (no fixed sleeps; polls a deadline).
function Wait-BeyzServiceStatus {
    param(
        [Parameter(Mandatory)][string]$Name,
        [Parameter(Mandatory)][ValidateSet('Running', 'Stopped')][string]$Status,
        [int]$TimeoutSeconds = 30
    )
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        $svc = Get-Service -Name $Name -ErrorAction SilentlyContinue
        if ($svc -and $svc.Status -eq $Status) { return $true }
        Start-Sleep -Milliseconds 250
    }
    return $false
}

# Bounded wait until the service no longer exists (post-uninstall SCM drain).
function Wait-BeyzServiceAbsent {
    param([Parameter(Mandatory)][string]$Name, [int]$TimeoutSeconds = 30)
    $deadline = (Get-Date).AddSeconds($TimeoutSeconds)
    while ((Get-Date) -lt $deadline) {
        if (-not (Test-BeyzServiceExists $Name)) { return $true }
        Start-Sleep -Milliseconds 250
    }
    return $false
}

# Distinct set of SIDs granted on a path (via .NET ACL, locale-independent).
function Get-BeyzAclSids {
    param([Parameter(Mandatory)][string]$Path)
    $acl = Get-Acl -LiteralPath $Path
    $sids = @()
    foreach ($ace in $acl.Access) {
        try {
            $sids += $ace.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value
        } catch {
            $sids += $ace.IdentityReference.Value
        }
    }
    return @($sids | Select-Object -Unique)
}

function Test-BeyzAclInheritanceBroken {
    param([Parameter(Mandatory)][string]$Path)
    return (Get-Acl -LiteralPath $Path).AreAccessRulesProtected
}

# Best-effort idempotent cleanup of a leftover agent service from a prior run.
function Remove-BeyzAgentServiceIfPresent {
    param([Parameter(Mandatory)][string]$AgentExe)
    if (Test-BeyzServiceExists 'BeyzBackupAgent') {
        & $AgentExe stop *> $null
        & $AgentExe uninstall *> $null
        [void](Wait-BeyzServiceAbsent 'BeyzBackupAgent' 30)
    }
}

Export-ModuleMember -Function * -Variable 'SID_*'
