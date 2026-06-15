<#
.SYNOPSIS
  S1-T30 Authenticode signing helper (signing-ready; UNSIGNED by default).

.DESCRIPTION
  Signs one or more files (agent.exe, updater.exe, setup.exe) with SHA-256 +
  an RFC3161 timestamp, then verifies the signature. The signing CERTIFICATE is
  supplied ONLY via environment (never committed, never logged):

    BEYZ_SIGN_THUMBPRINT   - thumbprint of a code-signing cert already in the
                             machine/user store (preferred; no password on disk),
                             OR
    BEYZ_SIGN_PFX_BASE64 + BEYZ_SIGN_PFX_PASSWORD - a base64-encoded PFX + its
                             password (decoded to a temp file deleted in finally).

  Behavior:
    * No cert configured + -RequireSigned NOT set  -> SKIP, exit 0 (the Sprint-1
      unsigned default; PR CI has no signing secrets).
    * No cert configured + -RequireSigned set       -> FAIL closed, exit 2
      (a release job that intends to sign must have a cert).
    * Cert configured + sign OR verify fails        -> FAIL closed, exit 1.

  The password is never written to a log or echoed; signtool args are not printed.

.PARAMETER Path        Files to sign.
.PARAMETER TimestampUrl RFC3161 timestamp server.
.PARAMETER RequireSigned Fail (not skip) when no certificate is configured.
.PARAMETER SkipVerify  Skip the post-sign `signtool verify /pa` (verify runs by default).
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string[]] $Path,
    [string] $TimestampUrl = 'http://timestamp.digicert.com',
    [switch] $RequireSigned,
    [switch] $SkipVerify
)

$ErrorActionPreference = 'Stop'

function Find-Signtool {
    $cmd = Get-Command signtool.exe -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $candidates = Get-ChildItem 'C:\Program Files (x86)\Windows Kits\10\bin\*\x64\signtool.exe' -ErrorAction SilentlyContinue |
        Sort-Object FullName -Descending
    if ($candidates) { return $candidates[0].FullName }
    throw 'signtool.exe not found (install the Windows SDK)'
}

$haveThumb = -not [string]::IsNullOrEmpty($env:BEYZ_SIGN_THUMBPRINT)
$havePfx   = -not [string]::IsNullOrEmpty($env:BEYZ_SIGN_PFX_BASE64)

if (-not ($haveThumb -or $havePfx)) {
    if ($RequireSigned) {
        Write-Error '[sign] no signing certificate configured but -RequireSigned was set (release job must provide a cert)'
        exit 2
    }
    Write-Host '[sign] no signing certificate configured -> UNSIGNED build (Sprint-1 default)'
    exit 0
}

$signtool = Find-Signtool
$pfxPath = $null
try {
    # Build the cert-selection args WITHOUT ever echoing the password.
    $certArgs = @()
    if ($haveThumb) {
        $certArgs = @('/sha1', $env:BEYZ_SIGN_THUMBPRINT)
    } else {
        $pfxPath = Join-Path $env:TEMP ('beyz-sign-' + [guid]::NewGuid().ToString('N') + '.pfx')
        [System.IO.File]::WriteAllBytes($pfxPath, [System.Convert]::FromBase64String($env:BEYZ_SIGN_PFX_BASE64))
        $certArgs = @('/f', $pfxPath, '/p', $env:BEYZ_SIGN_PFX_PASSWORD)
    }

    foreach ($file in $Path) {
        if (-not (Test-Path -LiteralPath $file)) { throw "[sign] file not found: $file" }
        Write-Host "[sign] signing $([System.IO.Path]::GetFileName($file))"
        # Note: $signArgs is NOT printed (it carries the cert selection / password).
        $signArgs = @('sign', '/fd', 'sha256', '/tr', $TimestampUrl, '/td', 'sha256') + $certArgs + @($file)
        & $signtool @signArgs | Out-Null
        if ($LASTEXITCODE -ne 0) { Write-Error "[sign] signtool sign failed for $file (exit $LASTEXITCODE)"; exit 1 }

        if (-not $SkipVerify) {
            & $signtool verify /pa $file | Out-Null
            if ($LASTEXITCODE -ne 0) { Write-Error "[sign] signtool verify /pa failed for $file (exit $LASTEXITCODE)"; exit 1 }
            Write-Host "[sign] verified $([System.IO.Path]::GetFileName($file))"
        }
    }
}
finally {
    if ($pfxPath -and (Test-Path -LiteralPath $pfxPath)) {
        Remove-Item -LiteralPath $pfxPath -Force -ErrorAction SilentlyContinue
    }
}
