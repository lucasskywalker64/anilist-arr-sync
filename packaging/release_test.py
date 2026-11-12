import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest


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
        for failure in [None, 'upload', 'download', 'remote verify', 'promotion']:
            with self.subTest(failure=failure), tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
                directory = Path(temp)
                (directory / 'checksums.txt').write_text('0'*64 + '  release.json\n')
                calls = []
                def runner(args, **kwargs):
                    calls.append(args)
                    stage = None
                    if args[:3] == ['gh', 'release', 'create']: stage = 'upload'
                    if args[:3] == ['gh', 'release', 'download']: stage = 'download'
                    if args[:4] == ['go', 'run', './cmd/release', 'verify'] and len(calls) > 1: stage = 'remote verify'
                    if 'tag_name=nightly' in args: stage = 'promotion'
                    if failure == stage and failure is not None:
                        raise subprocess.CalledProcessError(1, args)
                    value = json.dumps([{'tagName': 'nightly'}]) if args[:3] == ['gh', 'release', 'list'] else json.dumps({'id': 42})
                    return subprocess.CompletedProcess(args, 0, stdout=value)
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        self.module().publish_nightly(directory, 'v0.1.0-nightly.20261112010101.'+'a'*40, 'a'*40, runner)
                else:
                    self.module().publish_nightly(directory, 'v0.1.0-nightly.20261112010101.'+'a'*40, 'a'*40, runner)
                mutations = [args for args in calls if args[:3] == ['gh', 'release', 'delete'] or 'PATCH' in args]
                if failure in ['upload', 'download', 'remote verify']:
                    self.assertEqual(mutations, [])
                if failure == 'promotion':
                    self.assertFalse(any('force=true' in args for args in calls))
                if failure is None:
                    self.assertIn('force=true', calls[-1])
                    self.assertIn('tag_name=nightly', calls[-2])

    def test_nightly_skip_uses_signed_manifest_commit(self):
        module = self.module()
        self.assertFalse(module.changed('a'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('b'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('a'*40, None))
        with self.assertRaises(ValueError):
            module.changed('a'*40, {'commit': 'a'*40, 'channel': 'main'})


if __name__ == '__main__':
    unittest.main()
