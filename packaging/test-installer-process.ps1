$ErrorActionPreference = 'Stop'
$scriptFile = Join-Path $PSScriptRoot 'test-installer.ps1'
$tree = [Management.Automation.Language.Parser]::ParseFile($scriptFile, [ref]$null, [ref]$null)
$function = $tree.Find({ param($node) $node -is [Management.Automation.Language.FunctionDefinitionAst] -and $node.Name -eq 'InvokeInstallerProcess' }, $false)
. ([ScriptBlock]::Create($function.Extent.Text))
$fixtureRoot = Join-Path ([IO.Path]::GetTempPath()) ('installer-process-test-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $fixtureRoot | Out-Null
try {
    Set-Content -LiteralPath (Join-Path $fixtureRoot 'descendant-child.ps1') -Value 'Start-Sleep -Milliseconds 750; Set-Content -LiteralPath (Join-Path $PSScriptRoot ''descendant-complete'') -Value ''done'''
    foreach ($case in @(
        @{ Name = 'success'; Code = 'exit 0'; Timeout = 10000; Error = '' },
        @{ Name = 'log directory'; Code = '$log = $args | Where-Object { $_.StartsWith(''/LOG='') }; [IO.File]::WriteAllText($log.Substring(5), ''fixture log'')'; Timeout = 10000; Error = '' },
        @{ Name = 'descendant'; Code = '$child = Start-Process -FilePath (Join-Path $PSHOME ''pwsh.exe'') -ArgumentList @(''-NoProfile'', ''-File'', (''"'' + (Join-Path $PSScriptRoot ''descendant-child.ps1'') + ''"'')) -PassThru -WindowStyle Hidden; Set-Content -LiteralPath (Join-Path $PSScriptRoot ''descendant.pid'') -Value $child.Id'; Timeout = 10000; Error = '' },
        @{ Name = 'failure'; Code = 'exit 7'; Timeout = 10000; Error = 'exit code 7' },
        @{ Name = 'timeout'; Code = 'Start-Sleep -Seconds 30'; Timeout = 2000; Error = 'timed out' }
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
        if ($stage -eq 'descendant' -and !(Test-Path -LiteralPath (Join-Path $fixtureRoot 'descendant-complete'))) { throw 'Installer launcher returned before its child completed' }
    }
    $definition = Join-Path $fixtureRoot 'fixture.iss'
    Set-Content -LiteralPath $definition -Value @'
[Setup]
AppName=Installer process fixture
AppVersion=1.0
DefaultDirName={tmp}\installer-process-fixture
CreateUninstallRegKey=no
PrivilegesRequired=lowest
OutputBaseFilename=fixture
'@
    & ISCC.exe /Qp "/O$fixtureRoot" $definition | Out-Null
    if ($LASTEXITCODE -ne 0) { throw 'Cannot compile disposable installer process fixture' }
    $root = Join-Path $fixtureRoot 'native logs'
    $app = Join-Path $fixtureRoot 'native app'
    $stage = 'native install'
    InvokeInstallerProcess (Join-Path $fixtureRoot 'fixture.exe') @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=`"$app`"") 30000
    $uninstaller = Join-Path $app 'unins000.exe'
    if (!(Test-Path -LiteralPath $uninstaller)) { throw 'Native fixture did not create its uninstaller' }
    $stage = 'native uninstall'
    InvokeInstallerProcess $uninstaller @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART') 30000
    if (Test-Path -LiteralPath $uninstaller) { throw 'Native uninstaller launcher returned before self-deletion completed' }
    Write-Host 'Installer log creation, process-tree wait, success, nonzero exit, timeout, and native uninstall boundaries passed.'
} finally {
    $childIdFile = Join-Path $fixtureRoot 'descendant.pid'
    if (Test-Path -LiteralPath $childIdFile) { Wait-Process -Id ([int](Get-Content -LiteralPath $childIdFile)) -Timeout 5 -ErrorAction SilentlyContinue }
    $resolved = [IO.Path]::GetFullPath($fixtureRoot)
    if ([IO.Path]::GetDirectoryName($resolved) -ne [IO.Path]::GetFullPath([IO.Path]::GetTempPath()).TrimEnd('\') -or [IO.Path]::GetFileName($resolved) -notlike 'installer-process-test-*') { throw 'Unsafe process test cleanup target' }
    Remove-Item -LiteralPath $resolved -Recurse -Force
}
