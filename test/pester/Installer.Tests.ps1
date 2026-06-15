<#
  S1-T35 - installer-BACKED Windows Pester tests.

  Drives the real BeyzBackupSetup-*.exe (built by the installer-build CI job, or
  $env:BEYZ_SETUP_EXE) and asserts the S1-T29 contract end-to-end: silent install
  via /TOKENFILE=, the ProgramData tree + ACL matrix, the single LocalSystem
  service, no second (updater) service, token never in config.yaml or the
  installer log, updater run-once, and uninstall (default shred vs /KEEPSTATE).

  Each Describe performs its own clean install/uninstall cycle (self-cleaning
  BeforeAll/AfterAll), uses Temp-only scratch (token file + log), bounded SCM
  waits, and never contacts a real SaaS. Requires administrator (the GitHub
  windows runner is admin). The install TARGET is the runner's machine paths
  (the installer dictates C:\Program Files\BeyzBackup + C:\ProgramData\BeyzBackup);
  the runner is ephemeral and cleaned per Describe.
#>

BeforeAll {
    Import-Module (Join-Path $PSScriptRoot 'Beyz.Common.psm1') -Force

    $script:SetupExe = Get-BeyzSetupExe
    if (-not $script:SetupExe) {
        throw "setup.exe not found (build it via the installer-build job or set BEYZ_SETUP_EXE)"
    }

    $script:ProgramDir = Join-Path $env:ProgramFiles 'BeyzBackup'
    $script:DataRoot   = Join-Path $env:ProgramData 'BeyzBackup'
    $script:AgentExe   = Join-Path $script:ProgramDir 'beyz-backup-agent.exe'
    $script:UpdaterExe = Join-Path $script:ProgramDir 'beyz-backup-updater.exe'
    $script:ConfigYaml = Join-Path $script:DataRoot 'config.yaml'
    $script:StateDir   = Join-Path $script:DataRoot 'state'
    $script:TokenPath  = Join-Path $script:StateDir 'enroll-token'

    # Force-remove any leftover install so every Describe starts from clean.
    function Clear-BeyzInstall {
        $unins = Get-ChildItem -LiteralPath $script:ProgramDir -Filter 'unins*.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($unins) {
            Start-Process -FilePath $unins.FullName -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') -Wait -ErrorAction SilentlyContinue
            [void](Wait-BeyzServiceAbsent 'BeyzBackupAgent' 30)
        }
        if (Test-BeyzServiceExists 'BeyzBackupAgent') {
            & sc.exe stop 'BeyzBackupAgent' *> $null
            & sc.exe delete 'BeyzBackupAgent' *> $null
            [void](Wait-BeyzServiceAbsent 'BeyzBackupAgent' 30)
        }
        foreach ($d in @($script:DataRoot, $script:ProgramDir)) {
            if (Test-Path -LiteralPath $d) { Remove-Item -LiteralPath $d -Recurse -Force -ErrorAction SilentlyContinue }
        }
    }

    # Silent install with a Temp-only one-shot token file + a Temp-only /LOG.
    function Install-BeyzSilent {
        param([string[]]$ExtraArgs = @())
        $script:Token     = 'bzt_pester_' + ([guid]::NewGuid().ToString('N'))
        $script:TokenFile = Join-Path $env:TEMP ('beyz-token-' + [guid]::NewGuid().ToString('N') + '.txt')
        $script:LogFile   = Join-Path $env:TEMP ('beyz-install-' + [guid]::NewGuid().ToString('N') + '.log')
        Set-Content -LiteralPath $script:TokenFile -Value $script:Token -NoNewline -Encoding ascii
        $setupArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART',
                       "/TOKENFILE=$script:TokenFile", "/LOG=$script:LogFile") + $ExtraArgs
        $p = Start-Process -FilePath $script:SetupExe -ArgumentList $setupArgs -Wait -PassThru
        return $p.ExitCode
    }

    function Uninstall-Beyz {
        param([switch]$KeepState)
        $unins = Get-ChildItem -LiteralPath $script:ProgramDir -Filter 'unins*.exe' -ErrorAction SilentlyContinue | Select-Object -First 1
        if (-not $unins) { throw 'uninstaller not found' }
        $uninstArgs = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
        if ($KeepState) { $uninstArgs += '/KEEPSTATE' }
        Start-Process -FilePath $unins.FullName -ArgumentList $uninstArgs -Wait
        [void](Wait-BeyzServiceAbsent 'BeyzBackupAgent' 30)
    }
}

Describe 'Silent install (/TOKENFILE=) and the post-install contract' {

    BeforeAll {
        Clear-BeyzInstall
        $script:InstallExit = Install-BeyzSilent
    }
    AfterAll { Clear-BeyzInstall }

    It 'installs silently with /TOKENFILE= and exits 0' {
        $script:InstallExit | Should -Be 0
    }

    It 'lays down both binaries under Program Files\BeyzBackup' {
        Test-Path -LiteralPath $script:AgentExe   | Should -BeTrue
        Test-Path -LiteralPath $script:UpdaterExe | Should -BeTrue
    }

    It 'creates the ProgramData tree (config.yaml, state, logs, update)' {
        Test-Path -LiteralPath $script:ConfigYaml                       | Should -BeTrue
        Test-Path -LiteralPath $script:StateDir                        | Should -BeTrue
        Test-Path -LiteralPath (Join-Path $script:DataRoot 'logs')     | Should -BeTrue
        Test-Path -LiteralPath (Join-Path $script:DataRoot 'update')   | Should -BeTrue
    }

    It 'locks the ProgramData root to SYSTEM + Administrators (no Users), inheritance broken' {
        $sids = Get-BeyzAclSids $script:DataRoot
        $sids | Should -Contain $SID_SYSTEM
        $sids | Should -Contain $SID_ADMINS
        $sids | Should -Not -Contain $SID_USERS
        $sids | Should -Not -Contain $SID_EVERYONE
        Test-BeyzAclInheritanceBroken $script:DataRoot | Should -BeTrue
    }

    It 'locks state\ to SYSTEM + Administrators only' {
        $sids = Get-BeyzAclSids $script:StateDir
        $sids | Should -Contain $SID_SYSTEM
        $sids | Should -Contain $SID_ADMINS
        $sids | Should -Not -Contain $SID_USERS
        $sids | Should -Not -Contain $SID_EVERYONE
        Test-BeyzAclInheritanceBroken $script:StateDir | Should -BeTrue
    }

    It 'locks update\ to SYSTEM + Administrators only' {
        $update = Join-Path $script:DataRoot 'update'
        $sids = Get-BeyzAclSids $update
        $sids | Should -Contain $SID_SYSTEM
        $sids | Should -Contain $SID_ADMINS
        $sids | Should -Not -Contain $SID_USERS
        $sids | Should -Not -Contain $SID_EVERYONE
        Test-BeyzAclInheritanceBroken $update | Should -BeTrue
    }

    It 'grants Users read on logs\' {
        Get-BeyzAclSids (Join-Path $script:DataRoot 'logs') | Should -Contain $SID_USERS
    }

    It 'grants Users read on config.yaml' {
        Get-BeyzAclSids $script:ConfigYaml | Should -Contain $SID_USERS
    }

    It 'registers the single BeyzBackupAgent service as LocalSystem' {
        Test-BeyzServiceExists 'BeyzBackupAgent'        | Should -BeTrue
        Get-BeyzServiceStartName 'BeyzBackupAgent'      | Should -Be 'LocalSystem'
    }

    It 'does NOT register a BeyzBackupUpdater service (AC-42)' {
        Test-BeyzServiceExists 'BeyzBackupUpdater' | Should -BeFalse
    }

    It 'never writes the enrollment token into config.yaml' {
        (Get-Content -LiteralPath $script:ConfigYaml -Raw) | Should -Not -Match ([regex]::Escape($script:Token))
    }

    It 'never leaks the enrollment token into the installer log (AC-33)' {
        (Get-Content -LiteralPath $script:LogFile -Raw) | Should -Not -Match ([regex]::Escape($script:Token))
    }

    It 'locks the enroll-token file to SYSTEM only (when still present)' {
        # Without compiled pins the agent cannot complete enrollment, so the
        # one-shot token is normally still on disk; if present it MUST be SYSTEM-only.
        if (Test-Path -LiteralPath $script:TokenPath) {
            $sids = Get-BeyzAclSids $script:TokenPath
            $sids | Should -Contain $SID_SYSTEM
            $sids | Should -Not -Contain $SID_ADMINS
            $sids | Should -Not -Contain $SID_USERS
            $sids | Should -Not -Contain $SID_EVERYONE
        } else {
            Set-ItResult -Skipped -Because 'the agent already consumed the one-shot token'
        }
    }

    It 'the installed updater runs once and exits (no resident process)' {
        & $script:UpdaterExe --version | Out-Null
        (Get-Process -Name 'beyz-backup-updater' -ErrorAction SilentlyContinue) | Should -BeNullOrEmpty
    }

    It 'the installed service can be stopped' {
        & $script:AgentExe stop *> $null
        (Wait-BeyzServiceStatus 'BeyzBackupAgent' 'Stopped' 30) | Should -BeTrue
    }
}

Describe 'Uninstall - default shreds state' {

    BeforeAll {
        Clear-BeyzInstall
        Install-BeyzSilent | Out-Null
    }
    AfterAll { Clear-BeyzInstall }

    It 'removes the BeyzBackupAgent service and the state tree' {
        Uninstall-Beyz
        Test-BeyzServiceExists 'BeyzBackupAgent' | Should -BeFalse
        Test-Path -LiteralPath $script:StateDir  | Should -BeFalse
    }
}

Describe 'Uninstall - /KEEPSTATE preserves identity' {

    BeforeAll {
        Clear-BeyzInstall
        Install-BeyzSilent | Out-Null
        # Drop a marker so we can prove state\ survived.
        $script:Marker = Join-Path $script:StateDir 'pester-keepstate-marker'
        Set-Content -LiteralPath $script:Marker -Value 'keep' -Encoding ascii
    }
    AfterAll { Clear-BeyzInstall }

    It 'removes the service but preserves state\ under /KEEPSTATE' {
        Uninstall-Beyz -KeepState
        Test-BeyzServiceExists 'BeyzBackupAgent' | Should -BeFalse
        Test-Path -LiteralPath $script:StateDir  | Should -BeTrue
        Test-Path -LiteralPath $script:Marker    | Should -BeTrue
    }
}
