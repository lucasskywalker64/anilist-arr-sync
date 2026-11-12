"""GitHub publication boundary. Signing and verification run before mutations."""
import argparse
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
from urllib.error import HTTPError
from urllib.request import Request, urlopen


REPOSITORY = 'lucasskywalker64/anilist-arr-sync'


def run(args, **kwargs):
    return subprocess.run(args, check=True, text=True, **kwargs)


def api(path):
    request = Request('https://api.github.com/repos/' + REPOSITORY + path,
                      headers={'Authorization': 'Bearer ' + os.environ['GH_TOKEN'],
                               'Accept': 'application/vnd.github+json', 'User-Agent': 'anilist-arr-sync-release'})
    try:
        with urlopen(request, timeout=60) as response:
            return json.load(response)
    except HTTPError as error:
        if error.code == 404:
            return None
        raise


def changed(commit, manifest):
    if manifest is None:
        return True
    if manifest.get('channel') != 'develop' or not re.fullmatch('[0-9a-f]{40}', manifest.get('commit', '')):
        raise ValueError('Invalid previous nightly identity')
    return commit != manifest['commit']


def nightly_guard(commit):
    release = api('/releases/tags/nightly')
    if release is None:
        return True
    if release['draft'] or not release['prerelease']:
        raise ValueError('nightly must be a published prerelease')
    assets = {item['name']: item['browser_download_url'] for item in release['assets']}
    if len(assets) != len(release['assets']):
        raise ValueError('Duplicate nightly assets')
    Path('.release').mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(dir='.release') as temp:
        for name in ['release.json', 'checksums.txt', 'checksums.txt.minisig']:
            url = assets[name]
            if not url.startswith('https://github.com/' + REPOSITORY + '/releases/download/'):
                raise ValueError('Unexpected nightly asset URL')
            with urlopen(url, timeout=60) as response:
                data = response.read(2 * 1024 * 1024 + 1)
            if len(data) > 2 * 1024 * 1024:
                raise ValueError('Nightly metadata exceeds limit')
            Path(temp, name).write_bytes(data)
        run(['go', 'run', './cmd/release', 'verify-nightly', temp], stdout=subprocess.DEVNULL)
        return changed(commit, json.loads(Path(temp, 'release.json').read_bytes()))


def publish_nightly(directory, version, commit, command=run):
    # Exact directory uploaded to GitHub, including both installer architectures.
    command(['go', 'run', './cmd/release', 'verify', str(directory)])
    stage = 'nightly-stage-' + os.environ.get('GITHUB_RUN_ID', 'fixture') + '-' + os.environ.get('GITHUB_RUN_ATTEMPT', '1')
    names = [line.split('  ')[1] for line in (directory / 'checksums.txt').read_text().splitlines()]
    files = [str(directory / name) for name in sorted(names + ['checksums.txt', 'checksums.txt.minisig'])]
    command(['gh', 'release', 'create', stage, '--draft', '--prerelease', '--target', commit,
             '--title', version, '--notes', 'Nightly development build. Windows binaries omit Authenticode; checksums are Minisign-signed.', *files])
    # Re-download and authenticate the actual uploaded bytes while still draft.
    with tempfile.TemporaryDirectory(dir=directory.parent) as temp:
        command(['gh', 'release', 'download', stage, '--dir', temp])
        command(['go', 'run', './cmd/release', 'verify', temp])
    staged = command(['gh', 'api', f'repos/{REPOSITORY}/releases/tags/{stage}'], capture_output=True)
    release_id = json.loads(staged.stdout)['id']
    old = command(['gh', 'release', 'list', '--json', 'tagName', '--limit', '100'], capture_output=True)
    if any(item['tagName'] == 'nightly' for item in json.loads(old.stdout)):
        command(['gh', 'release', 'delete', 'nightly', '--yes'])
    # Only this final mutation exposes the new signed manifest as the checkpoint.
    # A failure leaves no new public identity; the guard retries on the next run.
    command(['gh', 'api', '--method', 'PATCH', f'repos/{REPOSITORY}/releases/{release_id}',
             '-f', 'tag_name=nightly', '-F', 'draft=false', '-F', 'prerelease=true',
             '-f', 'make_latest=false', '-f', 'target_commitish=' + commit])
    command(['gh', 'api', '--method', 'PATCH', f'repos/{REPOSITORY}/git/refs/tags/nightly',
             '-f', 'sha=' + commit, '-F', 'force=true'])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['nightly-guard', 'publish-nightly', 'bootstrap-guard'])
    args = parser.parse_args()
    if args.operation == 'nightly-guard':
        value = 'true' if nightly_guard(os.environ['RELEASE_COMMIT']) else 'false'
        with open(os.environ['GITHUB_OUTPUT'], 'a', encoding='utf-8') as output:
            output.write('changed=' + value + '\n')
    elif args.operation == 'bootstrap-guard':
        # Exactly one explicit exception. Fetch all pages; never infer from an
        # empty token, failed request, or a missing signing configuration.
        page = 1
        while True:
            releases = api(f'/releases?per_page=100&page={page}')
            if releases is None:
                raise ValueError('Cannot inspect bootstrap history')
            if any(not release['draft'] and '-bootstrap.' in release['tag_name'] for release in releases):
                raise ValueError('The one-time bootstrap release already exists')
            if len(releases) < 100:
                break
            page += 1
    else:
        publish_nightly(Path('.release/final'), os.environ['RELEASE_VERSION'], os.environ['RELEASE_COMMIT'])


if __name__ == '__main__':
    main()
