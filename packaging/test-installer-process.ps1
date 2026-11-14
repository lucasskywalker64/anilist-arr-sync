$ErrorActionPreference = 'Stop'
$scriptFile = Join-Path $PSScriptRoot 'test-installer.ps1'
$tree = [Management.Automation.Language.Parser]::ParseFile($scriptFile, [ref]$null, [ref]$null)
$function = $tree.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'InvokeInstallerProcess' }, $false)
. ([ScriptBlock]::Create($function.Extent.Text))
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('installer-process-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null
try {
    foreach ($case in @(
        @{ Name = 'success'; Code = 'exit 0'; Timeout = 10000; Error = '' },
        @{ Name = 'log directory'; Code = '$log = $args | Where-Object { $_.StartsWith(''/LOG='') }; [IO.File]::WriteAllText($log.Substring(5), ''fixture log'')'; Timeout = 10000; Error = '' },
        @{ Name = 'failure'; Code = 'exit 7'; Timeout = 10000; Error = 'exit code 7' },
        @{ Name = 'timeout'; Code = 'Start-Sleep -Seconds 30'; Timeout = 100; Error = 'timed out' }
    )) {
        $stage = $case.Name
        $root = Join-Path $fixtureRoot ($stage + ' logs')
        $fixture = Join-Path $fixtureRoot ($stage + '.ps1')
        Set-Content -LiteralPath $fixture -Value ('$ErrorActionPreference = ''Stop''; ' + $case.Code)
        $failure = ''
        try { InvokeInstallerProcess (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile', '-File', "`"$fixture`"") $case.Timeout }
        catch { $failure = $_.Exception.Message }
        if ($case.Error) {
            if (!$failure.Contains($case.Error)) { throw "Expected $($case.Error), got: $failure" }
        } elseif ($failure) { throw $failure }
        if ($stage -eq 'log directory') {
            $log = @(Get-ChildItem -LiteralPath $root -Filter '*.log')
            if ($log.Count -ne 1 -or (Get-Content -LiteralPath $log[0].FullName -Raw) -ne 'fixture log') { throw 'Installer log was not created at the supplied path' }
        }
    }
    Write-Host 'Installer log creation, success, nonzero exit, and timeout boundaries passed.'
} finally {
    $resolved = [IO.Path]::GetFullPath($fixtureRoot)
    if ([IO.Path]::GetDirectoryName($resolved) -ne [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') -or [IO.Path]::GetFileName($resolved) -notlike 'installer-process-test-*') { throw 'Unsafe process test cleanup target' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
