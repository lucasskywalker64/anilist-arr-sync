$ErrorActionPreference = 'Stop'
$scriptFile = Join-Path $PSScriptRoot 'test-installer.ps1'
$tree = [Management.Automation.Language.Parser]::ParseFile($scriptFile, [ref]$null, [ref]$null)
$function = $tree.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'InvokeInstallerProcess' }, $false)
. ([ScriptBlock]::Create($function.Extent.Text))
$root = Join-Path ([IO.Path]::GetTempPath()) ('installer-process-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $root | Out-Null
try {
    foreach ($case in @(
        @{ Name = 'success'; Code = 'exit 0'; Timeout = 10000; Error = '' },
        @{ Name = 'failure'; Code = 'exit 7'; Timeout = 10000; Error = 'exit code 7' },
        @{ Name = 'timeout'; Code = 'Start-Sleep -Seconds 30'; Timeout = 100; Error = 'timed out' }
    )) {
        $stage = $case.Name
        $fixture = Join-Path $root ($stage + '.ps1')
        Set-Content -LiteralPath $fixture -Value $case.Code
        $failure = ''
        try { InvokeInstallerProcess (Join-Path $PSHOME 'pwsh.exe') @('-NoProfile', '-File', "`"$fixture`"") $case.Timeout }
        catch { $failure = $_.Exception.Message }
        if ($case.Error) {
            if (!$failure.Contains($case.Error)) { throw "Expected $($case.Error), got: $failure" }
        } elseif ($failure) { throw $failure }
    }
    Write-Host 'Installer success, nonzero exit, and timeout boundaries passed.'
} finally {
    $resolved = [IO.Path]::GetFullPath($root)
    if ([IO.Path]::GetDirectoryName($resolved) -ne [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') -or [IO.Path]::GetFileName($resolved) -notlike 'installer-process-test-*') { throw 'Unsafe process test cleanup target' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
