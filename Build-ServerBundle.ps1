param(
    [string]$OutputDirectory = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_SERVER_PACKAGE_DIR)) { Join-Path ([IO.Path]::GetTempPath()) 'Clipman-server-package' } else { $env:CLIPMAN_SERVER_PACKAGE_DIR }),
    [string]$MacHost = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_MAC_HOST)) { 'mac' } else { $env:CLIPMAN_MAC_HOST }),
    [string]$MacRepo = $(if ([string]::IsNullOrWhiteSpace($env:CLIPMAN_MAC_REPO)) { '$HOME/clipman' } else { $env:CLIPMAN_MAC_REPO })
)

$ErrorActionPreference = 'Stop'
$OutputDirectory = [IO.Path]::GetFullPath($OutputDirectory)
$repoFullPath = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd('\')
if ($OutputDirectory.TrimEnd('\') -eq [IO.Path]::GetPathRoot($OutputDirectory).TrimEnd('\')) {
    throw 'OutputDirectory cannot be a filesystem root.'
}
if ($OutputDirectory.TrimEnd('\') -eq $repoFullPath -or
    $OutputDirectory.TrimEnd('\').StartsWith($repoFullPath + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw "OutputDirectory must be outside the source repository: $OutputDirectory"
}

function Get-ClipmanVersion {
    $versionFile = Join-Path $PSScriptRoot 'ClipmanServer\version.txt'
    $version = (Get-Content -LiteralPath $versionFile -Raw).Trim()
    if ($version -notmatch '^\d+\.\d+\.\d+$') {
        throw "Clipman Server version is invalid in $versionFile"
    }
    return $version
}

function Build-WindowsServerWrapper([string]$outputPath) {
    $csc = Join-Path $env:WINDIR 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
    if (-not (Test-Path -LiteralPath $csc)) {
        throw "Could not find the .NET Framework C# compiler at $csc"
    }

    $version = Get-ClipmanVersion
    $assemblyVersion = if ($version -match '^\d+\.\d+\.\d+$') { "$version.0" } else { $version }
    $generatedDirectory = Split-Path -Parent $outputPath
    $serverCore = Join-Path $generatedDirectory 'clipman-server-core.exe'
    $generatedAssemblyInfo = Join-Path $generatedDirectory 'GeneratedAssemblyInfo.cs'
    New-Item -ItemType Directory -Force -Path $generatedDirectory | Out-Null
    @(
        'using System.Reflection;',
        'using System.Runtime.InteropServices;',
        '',
        '[assembly: AssemblyTitle("Clipman Server")]',
        '[assembly: AssemblyDescription("Background server wrapper for Clipman")]',
        '[assembly: AssemblyCompany("Andre Louis")]',
        '[assembly: AssemblyProduct("Clipman Server")]',
        '[assembly: AssemblyCopyright("Copyright (c) Andre Louis")]',
        '[assembly: ComVisible(false)]',
        "[assembly: AssemblyVersion(`"$assemblyVersion`")]",
        "[assembly: AssemblyFileVersion(`"$assemblyVersion`")]",
        "[assembly: AssemblyInformationalVersion(`"$version`")]"
    ) -join [Environment]::NewLine | Set-Content -LiteralPath $generatedAssemblyInfo -Encoding UTF8

    $sources = @(Get-ChildItem -LiteralPath (Join-Path $PSScriptRoot 'ClipmanServerWindows') -Filter '*.cs' | Sort-Object Name | ForEach-Object { $_.FullName })
    $sources += $generatedAssemblyInfo
    if ($sources.Count -eq 0) {
        throw 'Windows Clipman Server wrapper source is missing.'
    }

    $references = @(
        'System.dll',
        'System.Core.dll',
        'System.Drawing.dll',
        'System.IO.Compression.FileSystem.dll',
        'System.Windows.Forms.dll',
        'System.Web.Extensions.dll'
    ) -join ','

    $previousGoOS = $env:GOOS
    $previousGoArch = $env:GOARCH
    $previousCGO = $env:CGO_ENABLED
    try {
        $env:GOOS = 'windows'
        $env:GOARCH = 'amd64'
        $env:CGO_ENABLED = '0'
        Push-Location (Join-Path $PSScriptRoot 'ClipmanServer')
        try {
            & go build -trimpath -ldflags "-s -w -X github.com/OnjLouis/Clipman/ClipmanServer/internal/buildinfo.Version=$version" -o $serverCore .\cmd\clipman-server
            if ($LASTEXITCODE -ne 0) { throw "Windows Go server core build failed with exit code $LASTEXITCODE" }
        }
        finally { Pop-Location }
    }
    finally {
        $env:GOOS = $previousGoOS
        $env:GOARCH = $previousGoArch
        $env:CGO_ENABLED = $previousCGO
    }

    & $csc /nologo /target:winexe /platform:x64 /out:$outputPath /reference:$references "/resource:$serverCore,ClipmanServerWrapper.clipman-server.exe" $sources
    if ($LASTEXITCODE -ne 0) {
        throw "Windows Clipman Server wrapper build failed with exit code $LASTEXITCODE"
    }

    # Use a child process so loading the EXE for verification does not lock build scratch.
    $verificationScript = Join-Path $generatedDirectory 'VerifyServerUpdater.ps1'
    @'
param([string]$AssemblyPath, [string]$Version)
$ErrorActionPreference = 'Stop'
$assembly = [Reflection.Assembly]::LoadFile($AssemblyPath)
$updateType = $assembly.GetType('ClipmanServerWrapper.ServerUpdateService', $true)
$versionText = $updateType.GetMethod('VersionText', [Reflection.BindingFlags]'NonPublic,Static')
if ($null -eq $versionText) {
    throw 'Windows Clipman Server updater tag validator was not found in the built executable.'
}
if ([string]$versionText.Invoke($null, @("v$Version")) -ne '') {
    throw 'Windows Clipman Server updater incorrectly accepted a Clipman client release tag.'
}
if ([string]$versionText.Invoke($null, @("server-v$Version")) -ne $Version) {
    throw 'Windows Clipman Server updater rejected its versioned server release tag.'
}
$releaseType = $assembly.GetType('ClipmanServerWrapper.ServerUpdateService+GitHubRelease', $true)
$assetType = $assembly.GetType('ClipmanServerWrapper.ServerUpdateService+GitHubAsset', $true)
$listType = [Type]::GetType('System.Collections.Generic.List`1').MakeGenericType($assetType)
$release = [Activator]::CreateInstance($releaseType, $true)
$assets = [Activator]::CreateInstance($listType)
foreach ($name in @("ClipmanServer-$Version.zip", "ClipmanServer-Windows-x64-$Version.zip")) {
    $asset = [Activator]::CreateInstance($assetType, $true)
    $assetType.GetProperty('Name').SetValue($asset, $name, $null)
    $assetType.GetProperty('BrowserDownloadUrl').SetValue($asset, "https://example.invalid/$name", $null)
    $assets.Add($asset)
}
$releaseType.GetProperty('Assets').SetValue($release, $assets, $null)
$findAsset = $updateType.GetMethod('FindServerAsset', [Reflection.BindingFlags]'NonPublic,Static')
$selected = $findAsset.Invoke($null, @($release, $Version))
$selectedName = [string]$assetType.GetProperty('Name').GetValue($selected, $null)
if ($selectedName -ne "ClipmanServer-Windows-x64-$Version.zip") {
    throw "Windows Clipman Server updater did not prefer its native release asset: $selectedName"
}
$assets.RemoveAt(1)
$selected = $findAsset.Invoke($null, @($release, $Version))
$selectedName = [string]$assetType.GetProperty('Name').GetValue($selected, $null)
if ($selectedName -ne "ClipmanServer-$Version.zip") {
    throw "Windows Clipman Server updater did not retain the Python-era transition fallback: $selectedName"
}
'@ | Set-Content -LiteralPath $verificationScript -Encoding UTF8
    & powershell -NoProfile -ExecutionPolicy Bypass -File $verificationScript -AssemblyPath $outputPath -Version $version
    if ($LASTEXITCODE -ne 0) {
        throw "Windows Clipman Server updater verification failed with exit code $LASTEXITCODE"
    }
}

$version = Get-ClipmanVersion
$zipPath = Join-Path $OutputDirectory "ClipmanServer-$version.zip"
$releaseDirectory = Join-Path $OutputDirectory "ClipmanServer-$version"
$layoutZipName = "ClipmanServer-$version-layout.zip"
$nativeAssetNames = @(
    "ClipmanServer-Windows-x64-$version.zip",
    "ClipmanServer-macOS-universal-$version.zip",
    "ClipmanServer-Linux-amd64-$version.tar.gz",
    "ClipmanServer-Linux-arm64-$version.tar.gz",
    "ClipmanServer-Linux-armv7-$version.tar.gz"
)
$localBuildDirectory = Join-Path ([IO.Path]::GetTempPath()) ('Clipman-server-build-' + [guid]::NewGuid().ToString('N'))
$windowsWrapperDist = Join-Path $localBuildDirectory 'Clipman Server.exe'
$remoteHome = (& ssh $MacHost 'printf %s "$HOME"').Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($remoteHome)) {
    throw "Could not determine the home folder on $MacHost."
}
$remoteRunDirectory = "$remoteHome/Projects/Codex/Temp/clipman/server-bundle-$version-$([guid]::NewGuid().ToString('N'))"
$remoteTempWindowsExe = "$remoteRunDirectory/windows-wrapper.exe"
$remoteMacDist = "$remoteRunDirectory/mac-dist"
$remoteCombinedDist = "$remoteRunDirectory/combined-dist"
$remoteReleaseDist = "$remoteRunDirectory/release-dist"
$remoteTempZip = "$remoteRunDirectory/ClipmanServer-$version.zip"

New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

try {
    New-Item -ItemType Directory -Force -Path $localBuildDirectory | Out-Null
    Build-WindowsServerWrapper $windowsWrapperDist

    Remove-Item -LiteralPath $zipPath -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $releaseDirectory -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $OutputDirectory $layoutZipName) -Force -ErrorAction SilentlyContinue
    foreach ($name in $nativeAssetNames) {
        Remove-Item -LiteralPath (Join-Path $OutputDirectory $name) -Force -ErrorAction SilentlyContinue
    }

    & ssh $MacHost "/bin/rm -rf '$remoteRunDirectory'; /bin/mkdir -p '$remoteMacDist' '$remoteCombinedDist' '$remoteReleaseDist'"
    if ($LASTEXITCODE -ne 0) {
        throw "Could not prepare Mac server bundle folders on $MacHost."
    }

    & scp $windowsWrapperDist "${MacHost}:$remoteTempWindowsExe"
    if ($LASTEXITCODE -ne 0) {
        throw "Could not copy Windows server wrapper to $MacHost."
    }

    & ssh $MacHost "cd `"$MacRepo`" && CLIPMAN_SERVER_MAC_DIST_DIR='$remoteMacDist' zsh ClipmanServerMac/Scripts/package-release.sh && CLIPMAN_SERVER_WINDOWS_EXE='$remoteTempWindowsExe' CLIPMAN_SERVER_MAC_APP='$remoteMacDist/Clipman Server.app' CLIPMAN_SERVER_RELEASE_OUTPUT_DIR='$remoteReleaseDist' zsh ClipmanServerMac/Scripts/package-release-layout.sh && CLIPMAN_SERVER_WINDOWS_EXE='$remoteTempWindowsExe' CLIPMAN_SERVER_MAC_APP='$remoteMacDist/Clipman Server.app' CLIPMAN_SERVER_COMBINED_OUTPUT_DIR='$remoteCombinedDist' zsh ClipmanServerMac/Scripts/package-combined-server.sh && cp '$remoteCombinedDist/ClipmanServer-$version.zip' '$remoteTempZip'"
    if ($LASTEXITCODE -ne 0) {
        throw "Mac-side Clipman Server bundle build failed on $MacHost."
    }

    & scp "${MacHost}:$remoteTempZip" $zipPath
    if ($LASTEXITCODE -ne 0) {
        throw "Could not copy Mac-built server bundle from $MacHost."
    }

    if (-not (Test-Path -LiteralPath $zipPath)) {
        throw "Server bundle ZIP was not created: $zipPath"
    }

    $layoutZipPath = Join-Path $OutputDirectory $layoutZipName
    & scp "${MacHost}:$remoteReleaseDist/$layoutZipName" $layoutZipPath
    if ($LASTEXITCODE -ne 0) {
        throw 'Could not copy the native release layout from the Mac build host.'
    }
    foreach ($name in $nativeAssetNames) {
        & scp "${MacHost}:$remoteReleaseDist/$name" (Join-Path $OutputDirectory $name)
        if ($LASTEXITCODE -ne 0) {
            throw "Could not copy native server release asset $name from the Mac build host."
        }
    }
    Expand-Archive -LiteralPath $layoutZipPath -DestinationPath $OutputDirectory -Force
    Remove-Item -LiteralPath $layoutZipPath -Force
    if (-not (Test-Path -LiteralPath (Join-Path $releaseDirectory 'release-manifest.json'))) {
        throw "Native release directory was not created: $releaseDirectory"
    }

    if (![string]::IsNullOrWhiteSpace($env:CLIPMAN_SERVER_BUILDS)) {
        New-Item -ItemType Directory -Force -Path $env:CLIPMAN_SERVER_BUILDS | Out-Null
        Copy-Item -LiteralPath $zipPath -Destination (Join-Path $env:CLIPMAN_SERVER_BUILDS (Split-Path -Leaf $zipPath)) -Force
        foreach ($name in $nativeAssetNames) {
            Copy-Item -LiteralPath (Join-Path $OutputDirectory $name) -Destination (Join-Path $env:CLIPMAN_SERVER_BUILDS $name) -Force
        }
    }

    Write-Host "Built release directory $releaseDirectory"
    Write-Host "Built transition asset $zipPath"
    foreach ($name in $nativeAssetNames) {
        Write-Host "Built native asset $(Join-Path $OutputDirectory $name)"
    }
}
finally {
    for ($attempt = 1; $attempt -le 10 -and (Test-Path -LiteralPath $localBuildDirectory); $attempt++) {
        try {
            Remove-Item -LiteralPath $localBuildDirectory -Recurse -Force -ErrorAction Stop
        }
        catch {
            if ($attempt -eq 10) {
                throw "Could not clean local Clipman Server build scratch: $localBuildDirectory"
            }
            Start-Sleep -Milliseconds 300
        }
    }
    & ssh $MacHost "/bin/rm -rf '$remoteRunDirectory'" 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "Could not remove one or more remote Clipman Server scratch paths from $MacHost."
    }
    $LASTEXITCODE = 0
}
