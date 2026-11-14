import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
from io import BytesIO


class PublicationBoundary(unittest.TestCase):
    def module(self):
        spec = importlib.util.spec_from_file_location('release', Path(__file__).with_name('release.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        return module

    def test_no_publication_after_failed_verification(self):
        calls = []
        def runner(args, **kwargs):
            calls.append(args)
            raise subprocess.CalledProcessError(1, args)
        with self.assertRaises(subprocess.CalledProcessError):
            self.module().publish_nightly(Path('.release/final'), 'v0.1.0-nightly.20261112010101.aaaaaaaaaaaa', 'a'*40, runner)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][:4], ['go', 'run', './cmd/release', 'verify'])

    def test_uploaded_bytes_verified_before_rolling_release_replacement(self):
        for failure in [None, 'upload', 'download', 'remote verify', 'promotion', 'reference', 'promotion response lost']:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
                directory = Path(temp)
                (directory / 'checksums.txt').write_text('0'*64 + '  release.json\n')
                calls = []
                previous = {'id': 7, 'tag_name': 'nightly', 'draft': False, 'prerelease': True}
                replacement = {'id': 42, 'tag_name': 'staged', 'draft': True, 'prerelease': True}
                refs = {'nightly': 'b'*40}
                failed = False
                def runner(args, **kwargs):
                    nonlocal failed
                    calls.append(args)
                    stage = None
                    if args[:3] == ['gh', 'release', 'create']: stage = 'upload'
                    if args[:3] == ['gh', 'release', 'download']: stage = 'download'
                    if args[:4] == ['go', 'run', './cmd/release', 'verify'] and len(calls) > 1: stage = 'remote verify'
                    if 'tag_name=nightly' in args: stage = 'promotion'
                    if args[:4] == ['gh', 'api', '--method', 'PATCH'] and args[4].endswith('/git/refs/tags/nightly') and 'sha=' + 'a'*40 in args:
                        stage = 'reference'
                    if failure == stage and failure is not None and not failed:
                        failed = True
                        raise subprocess.CalledProcessError(1, args)
                    if args[:3] == ['gh', 'release', 'delete']:
                        previous['draft'] = True
                    if args[:3] == ['gh', 'release', 'create']:
                        replacement['tag_name'] = args[3]
                    if args[:4] == ['gh', 'api', '--method', 'PATCH'] and '/releases/' in args[4]:
                        target = previous if args[4].endswith('/7') else replacement
                        for field in ['tag_name', 'draft', 'prerelease']:
                            values = [value.split('=', 1)[1] for value in args if value.startswith(field + '=')]
                            if values: target[field] = values[0] == 'true' if field != 'tag_name' else values[0]
                        if failure == 'promotion response lost' and 'draft=false' in args and not failed:
                            failed = True
                            raise subprocess.CalledProcessError(1, args)
                    if args[:4] == ['gh', 'api', '--method', 'POST'] and args[4].endswith('/git/refs'):
                        ref = next(value.removeprefix('ref=refs/tags/') for value in args if value.startswith('ref='))
                        refs[ref] = next(value[4:] for value in args if value.startswith('sha='))
                    if args[:4] == ['gh', 'api', '--method', 'PATCH'] and '/git/refs/tags/' in args[4]:
                        refs[args[4].split('/git/refs/tags/')[1]] = next(value[4:] for value in args if value.startswith('sha='))
                    if args[:3] == ['gh', 'release', 'list']:
                        value = [{'tagName': 'nightly'}]
                    elif '--paginate' in args:
                        value = [[previous.copy()]]
                    elif any('/git/matching-refs/' in value for value in args):
                        value = [{'ref': 'refs/tags/' + name, 'object': {'sha': sha}} for name, sha in refs.items()]
                    else:
                        value = {'id': 42}
                    return subprocess.CompletedProcess(args, 0, stdout=json.dumps(value))
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        self.module().publish_nightly(directory, 'v0.1.0-nightly.20261112010101.'+'a'*40, 'a'*40, runner)
                else:
                    self.module().publish_nightly(directory, 'v0.1.0-nightly.20261112010101.'+'a'*40, 'a'*40, runner)
                mutations = [args for args in calls if args[:3] == ['gh', 'release', 'delete'] or 'PATCH' in args]
                if failure in ['upload', 'download', 'remote verify']:
                    self.assertEqual(mutations, [])
                if failure:
                    self.assertFalse(previous['draft'], 'a failed replacement must retain the previous public release')
                    self.assertEqual(previous['tag_name'], 'nightly', 'recovery must restore rolling discovery')
                    self.assertEqual(refs['nightly'], 'b'*40, 'recovery must restore the previous source tag')
                    self.assertTrue(replacement['draft'], 'a failed replacement must not remain public')
                if failure is None:
                    self.assertEqual(refs['nightly'], 'a'*40)
                    self.assertFalse(replacement['draft'])
                    promotion = next(index for index, args in enumerate(calls) if 'draft=false' in args)
                    move = next(index for index, args in enumerate(calls) if 'sha=' + 'a'*40 in args)
                    self.assertLess(move, promotion, 'the source tag must point to the new commit before publication')

    def test_first_nightly_creates_rolling_tag_before_publication(self):
        calls = []
        def runner(args, **kwargs):
            calls.append(args)
            value = [[]] if '--paginate' in args else [] if any('/git/matching-refs/' in value for value in args) else {'id': 42}
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps(value))
        with tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
            directory = Path(temp)
            (directory / 'checksums.txt').write_text('0'*64 + '  release.json\n')
            self.module().publish_nightly(directory, 'fixture', 'a'*40, runner)
        create = next(index for index, args in enumerate(calls) if 'ref=refs/tags/nightly' in args)
        publish = next(index for index, args in enumerate(calls) if 'draft=false' in args)
        self.assertLess(create, publish)
        self.assertFalse(any(args[:3] == ['gh', 'release', 'delete'] for args in calls))

    def test_nightly_skip_uses_signed_manifest_commit(self):
        module = self.module()
        self.assertFalse(module.changed('a'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('b'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('a'*40, None))
        with self.assertRaises(ValueError):
            module.changed('a'*40, {'commit': 'a'*40, 'channel': 'main'})

    def test_unchanged_nightly_with_missing_installer_is_rebuilt(self):
        module = self.module()
        metadata = {
            'release.json': json.dumps({'version': 'v0.1.0-nightly.20261112010101.' + 'a'*40, 'commit': 'a'*40, 'channel': 'develop'}).encode(),
            'checksums.txt': b'fixture authenticated checksums',
            'checksums.txt.minisig': b'fixture signature',
        }
        release = {'draft': False, 'prerelease': True, 'assets': [
            {'name': name, 'browser_download_url': 'https://github.com/' + module.REPOSITORY + '/releases/download/nightly/' + name}
            for name in metadata
        ]}
        def download(url, **kwargs):
            return BytesIO(metadata[url.rsplit('/', 1)[1]])
        def api(path):
            return {'object': {'type': 'commit', 'sha': 'a'*40}} if path.startswith('/git/') else release
        with patch.object(module, 'api', side_effect=api), patch.object(module, 'urlopen', side_effect=download), patch.object(module, 'run'):
            self.assertTrue(module.nightly_guard('a'*40), 'an unchanged commit cannot hide missing release assets')

    def test_nightly_guard_verifies_final_bytes_and_fails_closed_on_bad_metadata(self):
        version = 'v0.1.0-nightly.20261112010101.' + 'a'*40
        installer = f'anilist-arr-sync_{version}_windows_arm64_setup.exe'
        files = {'release.json': json.dumps({'version': version, 'commit': 'a'*40, 'channel': 'develop'}).encode(),
                 'checksums.txt': b'fixture authenticated checksums', 'checksums.txt.minisig': b'fixture signature'}
        for arch in ['amd64', 'arm64']:
            for target in [f'linux_{arch}.tar.gz', f'windows_{arch}.zip', f'windows_{arch}_setup.exe']:
                files[f'anilist-arr-sync_{version}_{target}'] = b'fixture artifact'
        for failure in [None, 'artifact', 'signature', 'tag']:
            with self.subTest(failure=failure):
                module = self.module()
                release = {'draft': False, 'prerelease': True, 'assets': [
                    {'name': name, 'browser_download_url': 'https://github.com/' + module.REPOSITORY + '/releases/download/nightly/' + name}
                    for name in files]}
                def api(path):
                    return {'object': {'type': 'commit', 'sha': ('b' if failure == 'tag' else 'a')*40}} if path.startswith('/git/') else release
                def download(url, **kwargs):
                    name = url.rsplit('/', 1)[1]
                    return BytesIO(b'tampered' if failure == 'artifact' and name == installer else files[name])
                def verify(args, **kwargs):
                    if args[3] == 'verify-nightly' and failure == 'signature':
                        raise subprocess.CalledProcessError(1, args)
                    if args[3] == 'verify':
                        self.assertEqual(kwargs['env']['RELEASE_COMMIT'], 'a'*40)
                        if Path(args[4], installer).read_bytes() != b'fixture artifact':
                            raise subprocess.CalledProcessError(1, args)
                with patch.object(module, 'api', side_effect=api), patch.object(module, 'urlopen', side_effect=download), patch.object(module, 'run', side_effect=verify) as verifier:
                    if failure == 'signature':
                        with self.assertRaises(subprocess.CalledProcessError): module.nightly_guard('a'*40)
                    else:
                        self.assertEqual(module.nightly_guard('a'*40), failure is not None)
                    self.assertEqual(verifier.call_count, 1 if failure in ['signature', 'tag'] else 2)


if __name__ == '__main__':
    unittest.main()
