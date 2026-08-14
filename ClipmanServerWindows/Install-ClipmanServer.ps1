<#
.SYNOPSIS
Installs Clipman Server for the current user.

.DESCRIPTION
Copies clipmanserver.exe and keeps the Python-era Clipman Server.exe name as a
byte-identical compatibility alias. Server settings and databases under
LocalAppData are not modified. The separate Clipman CLI installation is not
changed.
#>
[CmdletBinding()]
param(
    [string]$SourceDirectory = $PSScriptRoot,
    [string]$InstallDirectory = $(Join-Path $env:LOCALAPPDATA 'Programs\Clipman Server'),
    [switch]$NoPath
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$source = [IO.Path]::GetFullPath($SourceDirectory)
$destination = [IO.Path]::GetFullPath($InstallDirectory)
if ($destination -eq [IO.Path]::GetPathRoot($destination)) {
    throw 'InstallDirectory cannot be a filesystem root.'
}
if ($destination.TrimEnd('\') -eq $source.TrimEnd('\') -or
    $destination.StartsWith($source.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'InstallDirectory must be outside the extracted release directory.'
}

$server = Join-Path $source 'clipmanserver.exe'
foreach ($required in @($server)) {
    if (-not (Test-Path -LiteralPath $required -PathType Leaf)) {
        throw "Required release file is missing: $required"
    }
}

New-Item -ItemType Directory -Path $destination -Force | Out-Null

function Install-File([string]$Source, [string]$Name) {
    $target = Join-Path $destination $Name
    $staged = $target + '.new'
    Copy-Item -LiteralPath $Source -Destination $staged -Force
    try {
        Move-Item -LiteralPath $staged -Destination $target -Force
    }
    catch {
        Remove-Item -LiteralPath $staged -Force -ErrorAction SilentlyContinue
        throw "Could not install $Name. Exit Clipman Server if it is running, then retry. $($_.Exception.Message)"
    }
}

Install-File $server 'clipmanserver.exe'
Install-File $server 'Clipman Server.exe'

$supportSource = Join-Path $source 'support'
if (Test-Path -LiteralPath $supportSource -PathType Container) {
    $supportDestination = Join-Path $destination 'support'
    New-Item -ItemType Directory -Path $supportDestination -Force | Out-Null
    Copy-Item -Path (Join-Path $supportSource '*') -Destination $supportDestination -Recurse -Force
}

if (-not $NoPath) {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    $entries = @($userPath -split ';' | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })
    $normalizedDestination = $destination.TrimEnd('\')
    $alreadyPresent = $entries | Where-Object {
        try {
            $entry = [Environment]::ExpandEnvironmentVariables($_.Trim().Trim('"'))
            [IO.Path]::GetFullPath($entry).TrimEnd('\') -eq $normalizedDestination
        }
        catch {
            # Preserve unusual existing PATH entries; they should not prevent
            # Clipman Server from being installed or added as a separate entry.
            $false
        }
    }
    if (-not $alreadyPresent) {
        $updated = (@($entries) + $destination) -join ';'
        [Environment]::SetEnvironmentVariable('Path', $updated, 'User')
        $env:Path = $destination + ';' + $env:Path
        Write-Output 'Added the install directory to the current user PATH.'
    }
}

Write-Output "Installed Clipman Server in $destination"
Write-Output 'Primary command: clipmanserver.exe'
Write-Output 'Compatibility name: Clipman Server.exe'
