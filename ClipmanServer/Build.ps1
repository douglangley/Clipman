param(
    [string]$OutputDirectory = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_SERVER_PACKAGE_DIR)) { Join-Path ([IO.Path]::GetTempPath()) 'clipman-server-build' } else { $env:CLIPMAN_SERVER_PACKAGE_DIR }),
    [string]$MacHost = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_MAC_HOST)) { 'mac' } else { $env:CLIPMAN_MAC_HOST }),
    [string]$MacRepo = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_MAC_REPO)) { '$HOME/clipman' } else { $env:CLIPMAN_MAC_REPO })
)

$ErrorActionPreference = 'Stop'
$builder = Join-Path (Split-Path -Parent $PSScriptRoot) 'Build-ServerBundle.ps1'
& $builder -OutputDirectory $OutputDirectory -MacHost $MacHost -MacRepo $MacRepo
if ($LASTEXITCODE -ne 0) {
    throw "Clipman Server release build failed with exit code $LASTEXITCODE"
}
