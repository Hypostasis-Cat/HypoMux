#requires -Version 5.1
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Collect-HypoMuxNetworkDiagnostics.ps1') -LibraryOnly
function Assert($Condition, [string]$Message) { if (-not $Condition) { throw $Message } }

$r = Invoke-PSProbe '[Console]::WriteLine("success"); [Console]::Error.WriteLine("diagnostic stderr"); exit 7' 8
Assert ($r.ExitCode -eq 7 -and $r.Stdout -match 'success' -and $r.Stderr -match 'diagnostic stderr') 'Child stdout/stderr/exit status lost'
$timer = [Diagnostics.Stopwatch]::StartNew()
$r = Invoke-PSProbe 'Start-Sleep -Seconds 30' 1
Assert ($r.TimedOut -and $r.ExitCode -eq -2 -and $timer.Elapsed.TotalSeconds -lt 12) 'Timeout did not terminate worker'
$r = Invoke-BoundedProcess 'C:\not-a-real-executable\missing.exe' @() 1
Assert ($r.ExitCode -eq -1 -and $r.Stderr) 'Missing tool not reported'

$fixtureDir = Join-Path ([IO.Path]::GetTempPath()) ('HypoMux diagnostic argv ' + [guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($fixtureDir) | Out-Null
try {
    $fixture = Join-Path $fixtureDir 'argv fixture.ps1'
    [IO.File]::WriteAllText($fixture, '[Console]::OutputEncoding = [Text.Encoding]::UTF8; ConvertTo-Json -InputObject @($args) -Compress')
    $values = @('a b', 'C:\space here\', 'a"b', 'literal$()&', '')
    $r = Invoke-BoundedProcess $powershellExe (@('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $fixture) + $values) 8
    $actual = ConvertFrom-Json -InputObject $r.Stdout
    Assert ($r.ExitCode -eq 0 -and $actual.Count -eq $values.Count) ("Argument count changed: " + ($r | ConvertTo-Json -Compress))
    for ($i = 0; $i -lt $values.Count; $i++) { Assert ($actual[$i] -ceq $values[$i]) ("Argument changed at index $i") }
} finally {
    # Exact temporary files we created; do not recursively remove a computed directory.
    if (Test-Path -LiteralPath $fixture) { Remove-Item -LiteralPath $fixture -Force }
    [IO.Directory]::Delete($fixtureDir)
}
$redacted = Protect-Text '{"secret":"hidden1","password":"hidden2","url":"https://user:pass@example.com/dns?key=hidden3"}'
Assert ($redacted -notmatch 'hidden[123]|user:pass') 'Secret redaction failed'
if ($env:USERPROFILE) {
    $redactedPath = Protect-Text ($env:USERPROFILE | ConvertTo-Json)
    Assert ($redactedPath -match '<USERPROFILE>') 'JSON-escaped profile path was not redacted'
}
$bad = [pscustomobject]@{ ExitCode=28; Stdout='' }
$good = [pscustomobject]@{ ExitCode=0; Stdout='TCP_OK' }
Assert ((Get-Finding $bad $bad $good) -match 'TLS') 'TCP success was confused with HTTPS success'
Assert ((Get-Finding $bad $good $good) -match 'FakeIP') 'Fixed-IP comparison missing'
Assert ((Get-Finding $good $bad $good) -match '成功') 'Successful HTTPS not recognized'
Write-Host 'PASS: subprocess streams/status, timeout, missing tool, argument escaping, redaction, evidence classification.'
