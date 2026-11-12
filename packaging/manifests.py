"""Generate installer consumers exclusively from authenticated final checksums."""
import json
import os
from pathlib import Path
import subprocess
import sys


def generate(directory, output=Path('.release')):
    subprocess.run(['go', 'run', './cmd/release', 'verify', str(directory)], check=True)
    manifest = json.loads((directory / 'release.json').read_bytes())
    version = manifest['version']
    if manifest['channel'] != 'main' or '-' in version:
        raise ValueError('Package managers accept stable production only')
    sums = dict(line.split('  ')[::-1] for line in (directory / 'checksums.txt').read_text().splitlines())
    base = 'https://github.com/lucasskywalker64/anilist-arr-sync/releases/download/' + version + '/'
    artifacts = {arch: f'anilist-arr-sync_{version}_windows_{arch}_setup.exe' for arch in ['amd64', 'arm64']}
    identifier = 'lucasskywalker64.AniList-Arr-Sync'
    common = f'PackageIdentifier: {identifier}\nPackageVersion: {version[1:]}\n'
    winget = output / 'winget'
    winget.mkdir(parents=True, exist_ok=True)
    (winget / f'{identifier}.yaml').write_text(common + 'DefaultLocale: en-US\nManifestType: version\nManifestVersion: 1.9.0\n', encoding='utf-8')
    locale = common + '''PackageLocale: en-US
Publisher: AniList-Arr-Sync contributors
PackageName: AniList-Arr-Sync
ShortDescription: Synchronize AniList with Sonarr and Radarr
PackageUrl: https://github.com/lucasskywalker64/anilist-arr-sync
ManifestType: defaultLocale
ManifestVersion: 1.9.0
'''
    locale += 'License: ' + json.dumps(os.environ['PACKAGE_LICENSE']) + '\n'
    (winget / f'{identifier}.locale.en-US.yaml').write_text(locale, encoding='utf-8')
    installers = common + '''InstallerType: inno
Scope: machine
UpgradeBehavior: install
InstallerSwitches:
  Custom: /UPDATEOWNER=winget /TYPE=service
Installers:
'''
    for arch, file in artifacts.items():
        installers += f'- Architecture: {"x64" if arch == "amd64" else "arm64"}\n  InstallerUrl: {base + file}\n  InstallerSha256: {sums[file]}\n'
    installers += 'ManifestType: installer\nManifestVersion: 1.9.0\n'
    (winget / f'{identifier}.installer.yaml').write_text(installers, encoding='utf-8')
    chocolatey = output / 'chocolatey'
    tools = chocolatey / 'tools'
    tools.mkdir(parents=True, exist_ok=True)
    (chocolatey / 'anilist-arr-sync.nuspec').write_text(f'''<?xml version="1.0"?>
<package xmlns="http://schemas.microsoft.com/packaging/2015/06/nuspec.xsd"><metadata>
<id>anilist-arr-sync</id><version>{version[1:]}</version><title>AniList-Arr-Sync</title>
<authors>AniList-Arr-Sync contributors</authors><projectUrl>https://github.com/lucasskywalker64/anilist-arr-sync</projectUrl>
<requireLicenseAcceptance>false</requireLicenseAcceptance><description>Synchronize AniList with Sonarr and Radarr.</description>
<tags>anilist sonarr radarr</tags></metadata><files><file src="tools/**" target="tools" /></files></package>
''', encoding='utf-8')
    install = "$ErrorActionPreference = 'Stop'\n"
    install += "$arm = ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') -or ($env:PROCESSOR_ARCHITEW6432 -eq 'ARM64')\n"
    install += "if ($arm) {\n"
    for index, (arch, file) in enumerate(reversed(list(artifacts.items()))):
        if index:
            install += '} else {\n'
        install += f"  $url = '{base + file}'\n  $checksum = '{sums[file]}'\n"
    install += '''}
Install-ChocolateyPackage -PackageName 'anilist-arr-sync' -FileType 'exe' -SilentArgs '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /UPDATEOWNER=chocolatey /TYPE=service' -Url $url -Checksum $checksum -ChecksumType 'sha256' -ValidExitCodes @(0)
'''
    (tools / 'chocolateyinstall.ps1').write_text(install, encoding='utf-8')
    (tools / 'chocolateyuninstall.ps1').write_text('''$ErrorActionPreference = 'Stop'
$keys = Get-UninstallRegistryKey -SoftwareName 'AniList-Arr-Sync*'
foreach ($key in $keys) {
    $file = $key.UninstallString.Trim('"')
    Uninstall-ChocolateyPackage -PackageName 'anilist-arr-sync' -FileType 'exe' -SilentArgs '/VERYSILENT /SUPPRESSMSGBOXES /NORESTART' -File $file -ValidExitCodes @(0)
}
''', encoding='utf-8')


if __name__ == '__main__':
    generate(Path(sys.argv[1]))
