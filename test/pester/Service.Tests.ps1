<#
  S1-T35 - installer-INDEPENDENT Windows service tests.

  Drives the agent's OWN control verbs (install/start/stop/restart/uninstall)
  against the cross-compiled binaries, with no installer involved. Deterministic
  assertions only: registration, LocalSystem, no-second-service, updater run-once,
  uninstall removal. "Service reaches RUNNING" is intentionally NOT asserted -
  the CI build has no compiled SPKI pins, so the started agent exits during boot;
  that stable assertion belongs to the installer-with-pins context.

  Requires administrator (the GitHub windows runner is admin). Self-cleaning.
#>

BeforeAll {
    Import-Module (Join-Path $PSScriptRoot 'Beyz.Common.psm1') -Force

    $script:BinDir     = Get-BeyzBinDir
    $script:AgentExe   = Join-Path $script:BinDir 'beyz-backup-agent.exe'
    $script:UpdaterExe = Join-Path $script:BinDir 'beyz-backup-updater.exe'

    if (-not (Test-Path -LiteralPath $script:AgentExe)) {
        throw "agent binary not found at $script:AgentExe (set BEYZ_BIN_DIR or build dist\windows first)"
    }

    # Temp-only scratch config (the service launches the agent with --config <this>).
    $script:TmpRoot    = Join-Path $env:TEMP ("beyz-svc-" + [guid]::NewGuid().ToString())
    New-Item -ItemType Directory -Force -Path $script:TmpRoot | Out-Null
    $script:ConfigPath = Join-Path $script:TmpRoot 'config.yaml'
    Copy-Item (Join-Path (Get-BeyzRepoRoot) 'configs\config.sample.yaml') $script:ConfigPath -Force

    # Clean any leftover service from a previously-failed run.
    Remove-BeyzAgentServiceIfPresent -AgentExe $script:AgentExe
}

AfterAll {
    if ($script:AgentExe) { Remove-BeyzAgentServiceIfPresent -AgentExe $script:AgentExe }
    if ($script:TmpRoot -and (Test-Path -LiteralPath $script:TmpRoot)) {
        Remove-Item -LiteralPath $script:TmpRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
}

Describe 'Agent service control verbs (no installer)' {

    It 'registers BeyzBackupAgent via the install verb' {
        & $script:AgentExe install --config $script:ConfigPath
        $LASTEXITCODE | Should -Be 0
        Test-BeyzServiceExists 'BeyzBackupAgent' | Should -BeTrue
    }

    It 'runs as LocalSystem' {
        Get-BeyzServiceStartName 'BeyzBackupAgent' | Should -Be 'LocalSystem'
    }

    It 'accepts start / stop / restart without deregistering the service' {
        # Best-effort: the agent exits during boot (no pins), so we do NOT assert
        # RUNNING - only that the lifecycle verbs are accepted and the registration
        # survives. stop converges deterministically.
        & $script:AgentExe start   *> $null
        & $script:AgentExe restart *> $null
        & $script:AgentExe stop    *> $null
        (Wait-BeyzServiceStatus 'BeyzBackupAgent' 'Stopped' 30) | Should -BeTrue
        Test-BeyzServiceExists 'BeyzBackupAgent' | Should -BeTrue
    }

    It 'removes the service via the uninstall verb (sc query -> 1060)' {
        & $script:AgentExe uninstall
        $LASTEXITCODE | Should -Be 0
        (Wait-BeyzServiceAbsent 'BeyzBackupAgent' 30) | Should -BeTrue
        Test-BeyzServiceExists 'BeyzBackupAgent' | Should -BeFalse
    }
}

Describe 'No second service + updater run-once (AC-42)' {

    It 'no BeyzBackupUpdater service is ever registered' {
        Test-BeyzServiceExists 'BeyzBackupUpdater' | Should -BeFalse
    }

    It 'the updater runs once and exits (no resident process)' {
        if (-not (Test-Path -LiteralPath $script:UpdaterExe)) {
            Set-ItResult -Skipped -Because 'updater binary not present in this build'
            return
        }
        & $script:UpdaterExe --version | Out-Null
        $LASTEXITCODE | Should -Be 0
        (Get-Process -Name 'beyz-backup-updater' -ErrorAction SilentlyContinue) | Should -BeNullOrEmpty
    }
}
