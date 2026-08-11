param(
    [Parameter(Mandatory = $true)][string]$ServerExecutable,
    [Parameter(Mandatory = $true)][string]$CliExecutable,
    [Parameter(Mandatory = $true)][string]$PythonServerScript,
    [Parameter(Mandatory = $true)][string]$TestRoot,
    [int]$InitialRecords = 300
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$workspace = (Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..\..')).Path
$resolvedTestRoot = [IO.Path]::GetFullPath($TestRoot)
if (-not $resolvedTestRoot.StartsWith($workspace + [IO.Path]::DirectorySeparatorChar) -or
    -not ([IO.Path]::GetFileName($resolvedTestRoot).StartsWith('.test-tmp-windows-cli-'))) {
    throw "Refusing non-test acceptance root: $resolvedTestRoot"
}
if (Test-Path -LiteralPath $resolvedTestRoot) {
    throw "Acceptance root already exists; use a new .test-tmp-windows-cli-* path: $resolvedTestRoot"
}
if ($InitialRecords -lt 200) {
    throw 'InitialRecords must be at least 200 for the full acceptance corpus.'
}

$serverExecutable = (Resolve-Path -LiteralPath $ServerExecutable).Path
$cliExecutable = (Resolve-Path -LiteralPath $CliExecutable).Path
$pythonServerScript = (Resolve-Path -LiteralPath $PythonServerScript).Path
New-Item -ItemType Directory -Path $resolvedTestRoot | Out-Null
$serverRoot = Join-Path $resolvedTestRoot 'server'
$clientA = Join-Path $resolvedTestRoot 'client-a\config.toml'
$clientB = Join-Path $resolvedTestRoot 'client-b\config.toml'
$settings = Join-Path $serverRoot 'settings.json'
$database = Join-Path $serverRoot 'data\clipman-history.clipdb'
$log = Join-Path $serverRoot 'logs\server.log'
$payload = Join-Path $resolvedTestRoot 'payload.txt'
$serverStdout = Join-Path $resolvedTestRoot 'server.stdout.log'
$serverStderr = Join-Path $resolvedTestRoot 'server.stderr.log'
$password = 'dummy-acceptance-password-2026'
$serverProcess = $null
$expected = @{}

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
    throw 'No free test port was found in the persistent server range.'
}

function Invoke-Cli {
    param([string]$Config, [string[]]$Arguments)
    $output = & $cliExecutable --config $Config @Arguments 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "clipman-cli failed ($LASTEXITCODE): $($Arguments -join ' ')`n$($output -join "`n")"
    }
    return ($output -join "`n")
}

function Wait-Healthy {
    param([string]$Url)
    $lastError = ''
    foreach ($attempt in 1..100) {
        try {
            $health = Invoke-RestMethod -Uri "$Url/api/v1/health" -TimeoutSec 2
            if ($health.Status -eq 'ok') { return }
        } catch {
            $lastError = $_.Exception.Message
        }
        Start-Sleep -Milliseconds 100
    }
    throw "Server did not become healthy: $lastError"
}

function Start-GoServer {
    $script:serverProcess = Start-Process -FilePath $serverExecutable -ArgumentList @('--config', $settings) `
        -RedirectStandardOutput $serverStdout -RedirectStandardError $serverStderr -WindowStyle Hidden -PassThru
    Wait-Healthy $script:serverUrl
}

function Start-PythonServer {
    $script:serverProcess = Start-Process -FilePath 'python' -ArgumentList @($pythonServerScript, '--config', $settings) `
        -RedirectStandardOutput $serverStdout -RedirectStandardError $serverStderr -WindowStyle Hidden -PassThru
    Wait-Healthy $script:serverUrl
}

function Stop-TestServer {
    if ($null -ne $script:serverProcess -and -not $script:serverProcess.HasExited) {
        Stop-Process -Id $script:serverProcess.Id
        $script:serverProcess.WaitForExit(5000) | Out-Null
    }
    $script:serverProcess = $null
}

function Add-Record {
    param([string]$Config, [string]$Name, [string]$Text, [string]$Group = '', [switch]$Pinned)
    [IO.File]::WriteAllText($payload, $Text, [Text.UTF8Encoding]::new($false))
    $arguments = @('put', '--file', $payload, '--name', $Name, '--duplicate', 'keep')
    if ($Group) { $arguments += @('--group', $Group) }
    if ($Pinned) { $arguments += '--pin' }
    Invoke-Cli $Config $arguments | Out-Null
    $script:expected[$Name] = $Text
}

function Remove-Record {
    param([string]$Config, [string]$Name)
    Invoke-Cli $Config @('rm', '--name', $Name, '--yes', '--json') | Out-Null
    $script:expected.Remove($Name)
}

function Assert-History {
    param([string]$Config, [string]$Label)
    $raw = Invoke-Cli $Config @('--json', 'list', '--all', '--kind', 'history')
    $items = @($raw | ConvertFrom-Json)
    if ($items.Count -ne $script:expected.Count) {
        throw "$Label entry count mismatch: expected $($script:expected.Count), got $($items.Count)"
    }
    $actual = @{}
    foreach ($item in $items) {
        if ($actual.ContainsKey($item.Name)) { throw "$Label duplicate name: $($item.Name)" }
        $actual[$item.Name] = [string]$item.Text
    }
    foreach ($name in $script:expected.Keys) {
        if (-not $actual.ContainsKey($name) -or $actual[$name] -cne $script:expected[$name]) {
            throw "$Label content mismatch for $name"
        }
    }
    $canonical = foreach ($name in $actual.Keys | Sort-Object) { "$name`0$($actual[$name])" }
    $bytes = [Text.Encoding]::UTF8.GetBytes($canonical -join "`n")
    return [Convert]::ToHexString([Security.Cryptography.SHA256]::HashData($bytes)).ToLowerInvariant()
}

try {
    $port = Find-TestPort
    $script:serverUrl = "http://127.0.0.1:$port"
    New-Item -ItemType Directory -Path $serverRoot | Out-Null
    $token = (& $serverExecutable --config $settings --host 127.0.0.1 --port $port --database $database --log $log --show-token).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $token) { throw 'Could not initialize isolated server settings.' }
    Start-GoServer

    Invoke-Cli $clientA @('--server', $serverUrl, '--password', $password, 'init', '--token', $token, '--save-password', 'config', '--machine', 'acceptance-a', '--non-interactive', '--force') | Out-Null
    Invoke-Cli $clientB @('--server', $serverUrl, '--password', $password, 'init', '--token', $token, '--save-password', 'config', '--machine', 'acceptance-b', '--non-interactive', '--force') | Out-Null

    foreach ($index in 0..($InitialRecords - 1)) {
        $name = 'seed-{0:d4}' -f $index
        $text = "Record $index`nUnicode: café 東京 😀`nCRLF follows:`r`nline-$index`nNUL:`0:end"
        Add-Record $clientA $name $text ('group-{0}' -f ($index % 7)) -Pinned:($index % 23 -eq 0)
        if (($index + 1) % 50 -eq 0) { Write-Output "seeded=$($index + 1)" }
    }
    Invoke-Cli $clientB @('sync', '--json') | Out-Null
    $hashA = Assert-History $clientA 'client A after seed'
    $hashB = Assert-History $clientB 'client B after seed'
    if ($hashA -ne $hashB) { throw 'Clients disagreed after the first A-to-B sync.' }

    foreach ($index in 0..39) { Add-Record $clientB ('from-b-{0:d3}' -f $index) "B record $index`nβ-direction" ('from-b') }
    foreach ($index in 0..19) { Remove-Record $clientB ('seed-{0:d4}' -f ($index * 3)) }
    Invoke-Cli $clientA @('status', '--refresh', '--json') | Out-Null
    $hashA = Assert-History $clientA 'client A after B changes'
    $hashB = Assert-History $clientB 'client B after B changes'
    if ($hashA -ne $hashB) { throw 'Clients disagreed after the B-to-A sync.' }

    foreach ($index in 0..19) { Add-Record $clientA ('from-a-{0:d3}' -f $index) "A return record $index`n↔ direction" ('from-a') }
    foreach ($index in 0..9) { Remove-Record $clientA ('seed-{0:d4}' -f (200 + $index)) }
    Invoke-Cli $clientB @('sync', '--json') | Out-Null

    Add-Record $clientA 'duplicate-probe' 'duplicate-content'
    Invoke-Cli $clientA @('put', '--text', 'duplicate-content', '--name', 'ignored-copy', '--duplicate', 'ignore', '--json') | Out-Null
    Invoke-Cli $clientA @('put', '--text', 'duplicate-content', '--name', 'moved-copy', '--duplicate', 'movetotop', '--json') | Out-Null
    Invoke-Cli $clientB @('sync', '--json') | Out-Null

    Invoke-Cli $clientA @('put', '--text', '{{year_full}}-acceptance', '--name', 'template-probe', '--template', '--json') | Out-Null
    $rawTemplate = Invoke-Cli $clientB @('get', '--kind', 'templates', '--name', 'template-probe', '--raw')
    if ($rawTemplate -cne '{{year_full}}-acceptance') { throw 'Template raw text was not preserved.' }
    $resolvedTemplate = Invoke-Cli $clientB @('get', '--kind', 'templates', '--name', 'template-probe')
    if ($resolvedTemplate -notmatch '^\d{4}-acceptance$') { throw 'Template resolution failed.' }
    Invoke-Cli $clientB @('rm', '--kind', 'templates', '--name', 'template-probe', '--yes') | Out-Null

    foreach ($name in @('seed-0100', 'from-a-005', 'from-b-015', 'duplicate-probe')) {
        $value = [string]((Invoke-Cli $clientB @('get', '--name', $name, '--json') | ConvertFrom-Json).Text)
        if ($value -cne $script:expected[$name]) { throw "get returned changed bytes for $name" }
    }
    Invoke-Cli $clientA @('list', '--all', '--group', 'from-b', '--porcelain') | Out-Null
    Invoke-Cli $clientA @('--json', 'list', '--all', '--search', '東京') | Out-Null
    Invoke-Cli $clientA @('get', '--search', 'seed-0150', '--first', '--json') | Out-Null
    Invoke-Cli $clientA @('status', '--json') | Out-Null

    Stop-TestServer
    Start-PythonServer
    Add-Record $clientA 'python-handoff' "written through Python server`nthen read by Go"
    Invoke-Cli $clientB @('sync', '--json') | Out-Null
    $pythonHash = Assert-History $clientB 'Python handoff'
    Stop-TestServer
    Start-GoServer
    Invoke-Cli $clientA @('sync', '--json') | Out-Null
    Invoke-Cli $clientB @('sync', '--json') | Out-Null
    $finalA = Assert-History $clientA 'final client A'
    $finalB = Assert-History $clientB 'final client B'
    if ($finalA -ne $finalB -or $finalA -ne $pythonHash) { throw 'Final Go/Python/Go hashes disagreed.' }

    $databaseHash = (Get-FileHash -LiteralPath (Get-ChildItem -LiteralPath (Split-Path $database) -Recurse -Filter '*.clipdb' | Select-Object -First 1).FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    [ordered]@{
        result = 'pass'
        initial_records = $InitialRecords
        final_history_records = $expected.Count
        logical_history_sha256 = $finalA
        encrypted_database_sha256 = $databaseHash
        go_python_go_handoff = $true
        clients = 2
        test_root = $resolvedTestRoot
    } | ConvertTo-Json
} finally {
    Stop-TestServer
}
