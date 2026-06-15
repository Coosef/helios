<#
.SYNOPSIS
  S1-T30 self-signed Authenticode SMOKE (no real secret).

.DESCRIPTION
  Proves the signing pipeline end-to-end using an EPHEMERAL self-signed
  code-signing certificate generated at job time - it requires NO real cert and
  NO repository/CI secret, so it is safe on PR CI. It:

    1. creates a self-signed CodeSigning cert,
    2. trusts it (Root + TrustedPublisher) so `signtool verify /pa`'s chain check
       passes for the self-signed signature,
    3. signs the agent.exe / updater.exe / setup.exe via installer/scripts/sign.ps1
       (-RequireSigned, so a missing cert would FAIL),
    4. verifies each, and
    5. removes the test cert from every store + deletes temp files.

  The real certificate path (BEYZ_SIGN_PFX_BASE64 / BEYZ_SIGN_THUMBPRINT from a
  protected GitHub Environment) is the SAME sign.ps1 code path - this only proves
  it with a throwaway cert.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string[]] $Path,
    [string] $SignScript = (Join-Path $PSScriptRoot '..\..\installer\scripts\sign.ps1')
)

$ErrorActionPreference = 'Stop'

$cert = New-SelfSignedCertificate -Type CodeSigningCert `
    -Subject 'CN=Beyz Backup Test Signer (SMOKE - not for production)' `
    -CertStoreLocation 'Cert:\CurrentUser\My' -KeyUsage DigitalSignature -KeyExportPolicy Exportable
$thumb = $cert.Thumbprint
$cerFile = Join-Path $env:TEMP ('beyz-smoke-' + $thumb + '.cer')

try {
    Export-Certificate -Cert $cert -FilePath $cerFile | Out-Null
    Import-Certificate -FilePath $cerFile -CertStoreLocation 'Cert:\CurrentUser\Root' | Out-Null
    Import-Certificate -FilePath $cerFile -CertStoreLocation 'Cert:\CurrentUser\TrustedPublisher' | Out-Null

    # sign.ps1 reads the cert by thumbprint from the store (no password on disk).
    $env:BEYZ_SIGN_THUMBPRINT = $thumb
    # Run the REAL sign.ps1 as a child process so its `exit` codes are captured
    # here (instead of unwinding this script). -RequireSigned makes a missing cert
    # a hard failure - we are proving the signed path.
    & pwsh -NoProfile -File $SignScript -Path $Path -RequireSigned
    $rc = $LASTEXITCODE
    if ($rc -ne 0) { throw "self-signed sign/verify smoke FAILED (sign.ps1 exit $rc)" }
    Write-Host "[smoke] self-signed sign + 'signtool verify /pa' PASSED for: $($Path -join ', ')"
}
finally {
    $env:BEYZ_SIGN_THUMBPRINT = $null
    foreach ($store in @('My', 'Root', 'TrustedPublisher')) {
        $p = "Cert:\CurrentUser\$store\$thumb"
        if (Test-Path $p) { Remove-Item $p -Force -ErrorAction SilentlyContinue }
    }
    if (Test-Path -LiteralPath $cerFile) { Remove-Item -LiteralPath $cerFile -Force -ErrorAction SilentlyContinue }
}
