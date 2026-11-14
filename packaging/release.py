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
        def download(name, limit):
            url = assets[name]
            if not url.startswith('https://github.com/' + REPOSITORY + '/releases/download/'):
                raise ValueError('Unexpected nightly asset URL')
            with urlopen(url, timeout=60) as response:
                data = response.read(limit + 1)
            if len(data) > limit:
                raise ValueError('Nightly asset exceeds limit')
            Path(temp, name).write_bytes(data)
        for name in ['release.json', 'checksums.txt', 'checksums.txt.minisig']:
            download(name, 2 * 1024 * 1024)
        run(['go', 'run', './cmd/release', 'verify-nightly', temp], stdout=subprocess.DEVNULL)
        manifest = json.loads(Path(temp, 'release.json').read_bytes())
        if changed(commit, manifest):
            return True
        # The signed manifest remains the identity. The discovery tag must also
        # agree before skipping a potentially interrupted publication.
        reference = api('/git/ref/tags/nightly')
        target = reference.get('object', {}) if reference else {}
        if target.get('type') != 'commit' or target.get('sha') != manifest['commit']:
            return True
        version = manifest['version']
        expected = {'release.json', 'checksums.txt', 'checksums.txt.minisig'}
        for arch in ['amd64', 'arm64']:
            expected.update({f'anilist-arr-sync_{version}_linux_{arch}.tar.gz',
                             f'anilist-arr-sync_{version}_windows_{arch}.zip',
                             f'anilist-arr-sync_{version}_windows_{arch}_setup.exe'})
        if set(assets) != expected:
            return True
        for name in sorted(expected - {'release.json', 'checksums.txt', 'checksums.txt.minisig'}):
            try:
                download(name, 256 * 1024 * 1024)
            except HTTPError as error:
                if error.code == 404:
                    return True
                raise
        environment = {**os.environ, 'RELEASE_MODE': 'nightly', 'RELEASE_VERSION': version, 'RELEASE_COMMIT': manifest['commit']}
        try:
            run(['go', 'run', './cmd/release', 'verify', temp], env=environment, stdout=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            return True
        return False


def publish_nightly(directory, version, commit, command=run):
    # Exact directory uploaded to GitHub, including both installer architectures.
    command(['go', 'run', './cmd/release', 'verify', str(directory)])
    stage = 'nightly-stage-' + os.environ.get('GITHUB_RUN_ID', 'fixture') + '-' + os.environ.get('GITHUB_RUN_ATTEMPT', '1')
    names = [line.split('  ')[1] for line in (directory / 'checksums.txt').read_text().splitlines()]
    files = [str(directory / name) for name in sorted(names + ['checksums.txt', 'checksums.txt.minisig'])]
    notes = ('Nightly development build. Windows binaries omit Authenticode; checksums are Minisign-signed.\n\n'
             'License: AGPL-3.0-only. No warranty.\n\n'
             f'[Exact application source](https://github.com/{REPOSITORY}/archive/{commit}.tar.gz). '
             f'[Dependency source and build instructions](https://github.com/{REPOSITORY}/blob/{commit}/SOURCE.md).\n\n'
             f'[Code signing policy](https://github.com/{REPOSITORY}/blob/{commit}/CODE_SIGNING.md) | '
             f'[Privacy policy](https://github.com/{REPOSITORY}/blob/{commit}/PRIVACY.md)')
    command(['gh', 'release', 'create', stage, '--draft', '--prerelease', '--target', commit,
             '--title', version, '--notes', notes, *files])
    # Re-download and authenticate the actual uploaded bytes while still draft.
    with tempfile.TemporaryDirectory(dir=directory.parent) as temp:
        command(['gh', 'release', 'download', stage, '--dir', temp])
        command(['go', 'run', './cmd/release', 'verify', temp])
    staged = command(['gh', 'api', f'repos/{REPOSITORY}/releases/tags/{stage}'], capture_output=True)
    release_id = json.loads(staged.stdout)['id']
    releases = command(['gh', 'api', '--paginate', '--slurp', f'repos/{REPOSITORY}/releases?per_page=100'], capture_output=True)
    previous = next((item for page in json.loads(releases.stdout) for item in page if item['tag_name'] == 'nightly'), None)
    references = command(['gh', 'api', f'repos/{REPOSITORY}/git/matching-refs/tags/nightly'], capture_output=True)
    reference = next((item for item in json.loads(references.stdout) if item['ref'] == 'refs/tags/nightly'), None)
    if previous and (previous['draft'] or not previous['prerelease'] or reference is None):
        raise ValueError('Previous nightly must be a published prerelease with a source tag')
    backup = stage + '-previous'

    def retag(identity, tag, draft=None):
        args = ['gh', 'api', '--method', 'PATCH', f'repos/{REPOSITORY}/releases/{identity}',
                '-f', 'tag_name=' + tag, '-f', 'make_latest=false']
        if draft is not None:
            args += ['-F', 'draft=' + str(draft).lower(), '-F', 'prerelease=true']
        command(args)

    def move(sha):
        command(['gh', 'api', '--method', 'PATCH', f'repos/{REPOSITORY}/git/refs/tags/nightly',
                 '-f', 'sha=' + sha, '-F', 'force=true'])

    archived = moved = promoting = False
    try:
        if previous:
            command(['gh', 'api', '--method', 'POST', f'repos/{REPOSITORY}/git/refs',
                     '-f', 'ref=refs/tags/' + backup, '-f', 'sha=' + reference['object']['sha']])
            # Preserve the old public release and its exact assets until success.
            archived = True
            retag(previous['id'], backup)
        moved = True
        if reference:
            move(commit)
        else:
            command(['gh', 'api', '--method', 'POST', f'repos/{REPOSITORY}/git/refs',
                     '-f', 'ref=refs/tags/nightly', '-f', 'sha=' + commit])
        promoting = True
        retag(release_id, 'nightly', draft=False)
    except Exception as error:
        # A request can be applied even when its response is lost. Undo attempted
        # mutations, including an ambiguous promotion, and try every recovery step.
        recovery = []
        if promoting:
            recovery.append(lambda: retag(release_id, stage, draft=True))
        if moved:
            if reference:
                recovery.append(lambda: move(reference['object']['sha']))
            else:
                recovery.append(lambda: command(['gh', 'api', '--method', 'DELETE', f'repos/{REPOSITORY}/git/refs/tags/nightly']))
        if archived:
            recovery.append(lambda: retag(previous['id'], 'nightly'))
        for restore in recovery:
            try:
                restore()
            except Exception:
                error.add_note('A nightly recovery request failed; the previous release and backup tag were retained.')
        raise
    # Cleanup cannot invalidate a successful publication. On failure, the old
    # release remains available under its backup tag for manual cleanup.
    if previous:
        try:
            command(['gh', 'release', 'delete', backup, '--yes', '--cleanup-tag'])
        except (OSError, subprocess.CalledProcessError):
            print('Nightly published; previous backup release retained because cleanup failed.')


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
