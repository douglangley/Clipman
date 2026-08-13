param(
    [Parameter(Mandatory = $true)][string]$ServerExecutable,
    [Parameter(Mandatory = $true)][string]$UpdaterExecutable,
    [Parameter(Mandatory = $true)][string]$CompatExecutable,
    [Parameter(Mandatory = $true)][string]$CliExecutable,
    [Parameter(Mandatory = $true)][string]$TestRoot,
    [string]$HistoricalPackages = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$workspace = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$resolvedTestRoot = [IO.Path]::GetFullPath($TestRoot)
if (-not $resolvedTestRoot.StartsWith($workspace + [IO.Path]::DirectorySeparatorChar) -or
    -not ([IO.Path]::GetFileName($resolvedTestRoot).StartsWith('.test-tmp-clipman-server-package-windows-'))) {
    throw "Refusing non-test package root: $resolvedTestRoot"
}
if (Test-Path -LiteralPath $resolvedTestRoot) {
    throw "Package root already exists; use a new .test-tmp-clipman-server-package-windows-* path: $resolvedTestRoot"
}

$serverExecutable = (Resolve-Path -LiteralPath $ServerExecutable).Path
$updaterExecutable = (Resolve-Path -LiteralPath $UpdaterExecutable).Path
$compatExecutable = (Resolve-Path -LiteralPath $CompatExecutable).Path
$cliExecutable = (Resolve-Path -LiteralPath $CliExecutable).Path
$coverageManifest = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\compat\coverage.json')).Path
$historicalManifest = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\compat\historical-releases.json')).Path
if ($HistoricalPackages) {
    $HistoricalPackages = (Resolve-Path -LiteralPath $HistoricalPackages).Path
}

New-Item -ItemType Directory -Path $resolvedTestRoot | Out-Null
[IO.File]::WriteAllText(
    (Join-Path $resolvedTestRoot 'clipman-package-test.json'),
    '{"purpose":"clipman-server-package-compat","version":1}',
    [Text.UTF8Encoding]::new($false)
)
$installRoot = Join-Path $resolvedTestRoot 'installed'
$installedServer = Join-Path $installRoot 'clipman-server.exe'
$serverRoot = Join-Path $resolvedTestRoot 'server-state'
$settings = Join-Path $serverRoot 'clipman-server-settings.json'
$database = Join-Path $serverRoot 'data\clipman-history.clipdb'
$serverLog = Join-Path $serverRoot 'logs\clipman-server.log'
$tokenFile = Join-Path $resolvedTestRoot 'token.txt'
$stdoutLog = Join-Path $resolvedTestRoot 'server.stdout.log'
$stderrLog = Join-Path $resolvedTestRoot 'server.stderr.log'
$serverProcess = $null

function Find-TestPort {
    foreach ($candidate in 23000..49000 | Sort-Object { Get-Random }) {
        $listener = [Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback, $candidate)
        try {
            $listener.Start()
            return $candidate
        } catch {
        } finally {
            $listener.Stop()
        }
    }
    throw 'No free package-test port was found in the persistent server range.'
}

function Get-GoArchitecture {
    switch ([Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
        'X64' { return 'amd64' }
        'Arm64' { return 'arm64' }
        default { throw "Unsupported Windows package-test architecture: $([Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
    }
}

function New-NativePackage {
    param(
        [Parameter(Mandatory = $true)][string]$PackagePath,
        [Parameter(Mandatory = $true)][string]$ProgramPath,
        [Parameter(Mandatory = $true)][string]$Version,
        [Parameter(Mandatory = $true)][string]$StagingName
    )
    $staging = Join-Path $resolvedTestRoot $StagingName
    $binaryDirectory = Join-Path $staging 'bin'
    New-Item -ItemType Directory -Path $binaryDirectory | Out-Null
    $stagedProgram = Join-Path $binaryDirectory 'clipman-server.exe'
    Copy-Item -LiteralPath $ProgramPath -Destination $stagedProgram
    $digest = (Get-FileHash -LiteralPath $stagedProgram -Algorithm SHA256).Hash.ToLowerInvariant()
    $manifest = [ordered]@{
        format_version = 2
        name = 'Clipman Server'
        version = $Version
        artifacts = @([ordered]@{
            os = 'windows'
            architecture = (Get-GoArchitecture)
            path = 'bin/clipman-server.exe'
            sha256 = $digest
        })
    }
    [IO.File]::WriteAllText(
        (Join-Path $staging 'manifest-v2.json'),
        ($manifest | ConvertTo-Json -Depth 5),
        [Text.UTF8Encoding]::new($false)
    )
    Compress-Archive -Path (Join-Path $staging '*') -DestinationPath $PackagePath
    return $digest
}

function Wait-Healthy {
    $lastError = ''
    foreach ($attempt in 1..100) {
        try {
            $health = Invoke-RestMethod -Uri "$script:serverUrl/api/v1/health" -TimeoutSec 2
            if ($health.Status -eq 'ok') {
                return
            }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Installed server did not become healthy: $lastError"
}

function Start-TestServer {
    $script:serverProcess = Start-Process -FilePath $installedServer -ArgumentList @('--config', $settings) `
        -RedirectStandardOutput $stdoutLog -RedirectStandardError $stderrLog -WindowStyle Hidden -PassThru
    Wait-Healthy
}

function Stop-TestServer {
    if ($null -ne $script:serverProcess -and -not $script:serverProcess.HasExited) {
        Stop-Process -Id $script:serverProcess.Id
        $script:serverProcess.WaitForExit(5000) | Out-Null
    }
    $script:serverProcess = $null
}

function Invoke-PackageMode {
    param([Parameter(Mandatory = $true)][string]$Seed)
    $arguments = @(
        '--mode', 'package',
        '--coverage', $coverageManifest,
        '--historical-releases', $historicalManifest,
        '--go-server', $installedServer,
        '--clipman-cli', $cliExecutable,
        '--server-url', $script:serverUrl,
        '--test-root', $resolvedTestRoot,
        '--token-file', $tokenFile,
        '--seed', $Seed
    )
    if ($HistoricalPackages) {
        $arguments += @('--historical-packages', $HistoricalPackages)
    }
    $output = & $compatExecutable @arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Package compatibility mode failed ($LASTEXITCODE):`n$($output -join "`n")"
    }
    return (($output -join "`n") | ConvertFrom-Json)
}

function Get-StateSnapshot {
    $snapshot = [ordered]@{}
    if (-not (Test-Path -LiteralPath $serverRoot)) {
        return $snapshot
    }
    foreach ($file in Get-ChildItem -LiteralPath $serverRoot -Recurse -File | Sort-Object FullName) {
        $relative = [IO.Path]::GetRelativePath($serverRoot, $file.FullName)
        $snapshot[$relative] = [ordered]@{
            length = $file.Length
            sha256 = (Get-FileHash -LiteralPath $file.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
        }
    }
    return $snapshot
}

try {
    $version = (& $serverExecutable --version).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $version) {
        throw 'Could not read the native server version.'
    }
    $packageVersion = ($version -replace '-.*$', '')
    if ($packageVersion -notmatch '^\d+\.\d+(\.\d+){0,2}$') {
        throw "The native server version cannot be represented in a stable package manifest: $version"
    }
    $goodPackage = Join-Path $resolvedTestRoot 'native-good.zip'
    $installedDigest = New-NativePackage -PackagePath $goodPackage -ProgramPath $serverExecutable -Version $packageVersion -StagingName 'good-package'
    $installOutput = & $updaterExecutable --package $goodPackage --target $installedServer 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "Native package installation failed ($LASTEXITCODE):`n$($installOutput -join "`n")"
    }
    $actualInstalledDigest = (Get-FileHash -LiteralPath $installedServer -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($actualInstalledDigest -ne $installedDigest) {
        throw 'The installed native server digest differs from the package artifact.'
    }

    $port = Find-TestPort
    $script:serverUrl = "http://127.0.0.1:$port"
    $token = (& $installedServer --config $settings --host 127.0.0.1 --port $port --database $database --log $serverLog --show-token).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $token) {
        throw 'Could not initialize isolated installed-server settings.'
    }
    [IO.File]::WriteAllText($tokenFile, $token + "`n", [Text.UTF8Encoding]::new($false))
    Start-TestServer
    $afterInstall = Invoke-PackageMode -Seed 'installed-native'
    Stop-TestServer

    $stateBeforeRollback = Get-StateSnapshot
    $stateBeforeRollbackJSON = $stateBeforeRollback | ConvertTo-Json -Depth 5 -Compress
    $badProgram = Join-Path $resolvedTestRoot 'unhealthy-server.exe'
    [IO.File]::WriteAllBytes($badProgram, [Text.Encoding]::UTF8.GetBytes('not a runnable server'))
    $badPackage = Join-Path $resolvedTestRoot 'native-unhealthy.zip'
    New-NativePackage -PackagePath $badPackage -ProgramPath $badProgram -Version '99.99.99' -StagingName 'bad-package' | Out-Null
    $rollbackOutput = & $updaterExecutable --package $badPackage --target $installedServer --health-url "$script:serverUrl/api/v1/health" 2>&1
    $rollbackExitCode = $LASTEXITCODE
    if ($rollbackExitCode -eq 0 -or ($rollbackOutput -join "`n") -notmatch 'rollback completed') {
        throw "Forced rollback did not report the expected failure ($rollbackExitCode):`n$($rollbackOutput -join "`n")"
    }
    $restoredDigest = (Get-FileHash -LiteralPath $installedServer -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($restoredDigest -ne $installedDigest) {
        throw 'Forced rollback did not restore the installed native server bytes.'
    }
    if (Test-Path -LiteralPath ($installedServer + '.rollback')) {
        throw 'Forced rollback left a stale rollback executable.'
    }
    $stateAfterRollbackJSON = (Get-StateSnapshot) | ConvertTo-Json -Depth 5 -Compress
    if ($stateAfterRollbackJSON -cne $stateBeforeRollbackJSON) {
        throw 'A failed program update changed server settings, data, certificates, connection files, or logs.'
    }

    Start-TestServer
    $afterRollback = Invoke-PackageMode -Seed 'restored-native'
    [ordered]@{
        result = 'pass'
        version = $version
        installed_sha256 = $installedDigest
        successful_install_package_mode = $afterInstall.package.operations
        forced_rollback_exit_code = $rollbackExitCode
        rollback_preserved_server_state = $true
        restored_package_mode = $afterRollback.package.operations
        historical_packages_verified = @($afterRollback.historical_packages).Count
        test_root = $resolvedTestRoot
    } | ConvertTo-Json -Depth 5
} finally {
    Stop-TestServer
}
