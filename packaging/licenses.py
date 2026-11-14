"""Preserve audited runtime license texts; fail CI when their notices change."""
import argparse
import json
import os
from pathlib import Path
import subprocess


GO_LICENSES = {
    'aead.dev/minisign': 'MIT',
    'github.com/dustin/go-humanize': 'MIT',
    'github.com/google/uuid': 'BSD-3-Clause',
    'github.com/mattn/go-isatty': 'MIT',
    'github.com/ncruces/go-strftime': 'MIT',
    'github.com/remyoudompheng/bigfft': 'BSD-3-Clause',
    'golang.org/x/crypto': 'BSD-3-Clause',
    'golang.org/x/mod': 'BSD-3-Clause',
    'golang.org/x/sys': 'BSD-3-Clause',
    'modernc.org/libc': 'BSD-3-Clause',
    'modernc.org/mathutil': 'BSD-3-Clause',
    'modernc.org/memory': 'BSD-3-Clause',
    'modernc.org/sqlite': 'BSD-3-Clause',
}
# clsx is bundled by the Svelte 5 runtime.
WEB_LICENSES = {'clsx': 'MIT', 'svelte': 'MIT', 'lucide-svelte': 'ISC'}


def notices():
    result = ['Third-party software notices\n\nThe components below retain their own licenses.\n'
              'The application is licensed under AGPL-3.0-only; these notices do not relicense dependencies.\n'
              'Upstream license and notice files are included verbatim, including repeated inherited notices.\n'
              'Upstream modernc.org notices also describe test-only and module-graph dependencies\n'
              '(including Apache-2.0 and MPL-2.0 entries) that are not compiled into this application.\n']
    goroot = subprocess.check_output(['go', 'env', 'GOROOT'], text=True).strip()
    result.append('\n=== Go standard library and runtime (BSD-3-Clause) ===\nSource: https://go.dev/dl/ (the executable license page and --license identify the exact Go version)\n\n' + Path(goroot, 'LICENSE').read_text(encoding='utf-8'))
    modules = {}
    for target, architecture in [('linux', 'amd64'), ('linux', 'arm64'), ('windows', 'amd64'), ('windows', 'arm64')]:
        environment = {**os.environ, 'GOOS': target, 'GOARCH': architecture, 'CGO_ENABLED': '0'}
        listing = subprocess.check_output(['go', 'list', '-deps', '-f', '{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}{{end}}', './cmd/anilist-arr-sync'], text=True, env=environment)
        for line in listing.splitlines():
            if '|' not in line:
                continue
            name, version, directory = line.split('|')
            if version:
                modules[name] = (version, Path(directory))
    if set(modules) != set(GO_LICENSES):
        raise ValueError('Runtime Go dependencies changed; audit GO_LICENSES: ' + ', '.join(sorted(set(modules) ^ set(GO_LICENSES))))
    for name, (version, directory) in sorted(modules.items()):
        files = sorted(path for path in directory.iterdir() if path.is_file() and path.name.upper().startswith(('LICENSE', 'COPYING', 'NOTICE', 'COPYRIGHT')))
        if not files:
            raise ValueError('Missing license text: ' + name)
        result.append(f'\n=== {name}@{version} ({GO_LICENSES[name]}) ===\nSource: https://proxy.golang.org/{name}/@v/{version}.zip\n')
        for path in files:
            result.append('\n--- ' + path.name + ' ---\n\n' + path.read_text(encoding='utf-8'))
    lock = json.loads(Path('web/package-lock.json').read_text(encoding='utf-8'))
    frontend = json.loads(Path('web/package.json').read_text(encoding='utf-8'))
    if set(frontend.get('dependencies', {})) | {'svelte', 'clsx'} != set(WEB_LICENSES):
        raise ValueError('Frontend runtime dependencies changed; audit WEB_LICENSES')
    for name, expected in sorted(WEB_LICENSES.items()):
        package = Path('web/node_modules', name)
        metadata = json.loads((package / 'package.json').read_text(encoding='utf-8'))
        pinned = lock['packages']['node_modules/' + name]
        if metadata['version'] != pinned['version'] or metadata['license'] != expected:
            raise ValueError('Frontend dependency changed; audit its license: ' + name)
        license_file = package / {'svelte': 'LICENSE.md', 'clsx': 'license'}.get(name, 'LICENSE')
        version = metadata['version']
        result.append(f'\n=== {name}@{version} ({expected}) ===\nSource: https://registry.npmjs.org/{name}/-/{name}-{version}.tgz\n\n' + license_file.read_text(encoding='utf-8'))
    return '\n'.join(part.rstrip() for part in result).replace('\r\n', '\n') + '\n'


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true')
    args = parser.parse_args()
    output = notices()
    destination = Path('THIRD_PARTY_NOTICES.txt')
    if args.check:
        if not destination.is_file() or destination.read_text(encoding='utf-8') != output:
            raise SystemExit('Third-party notices are stale. Audit dependencies and run python packaging/licenses.py')
    else:
        destination.write_text(output, encoding='utf-8', newline='\n')
