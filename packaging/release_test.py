import importlib.util
from contextlib import chdir
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import textwrap
import unittest
from unittest.mock import patch
from io import BytesIO
from urllib.error import HTTPError


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
            self.module().publish_nightly(Path('.release/final'), 'v0.1.0-nightly.20261116.123', 'a'*40, runner)
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0][:4], ['go', 'run', './cmd/release', 'verify'])

    def test_workflow_versions_use_utc_date_and_reuse_run_number_on_reruns(self):
        workflow = Path(__file__).resolve().parents[1] / '.github/workflows/release-nightly.yml'
        step = workflow.read_text().split('        id: identity\n', 1)[1]
        script = textwrap.dedent(step.split('        run: |\n', 1)[1].split('      - name:', 1)[0])
        bash = str(Path(shutil.which('git')).resolve().parents[1] / 'bin/bash.exe') if os.name == 'nt' else 'bash'
        fixtures = ('git() { if [ "$1" = rev-parse ]; then printf "%040d" 0; else echo v0.1.0; fi; }\n'
                    'date() { test "$*" = "-u +%Y%m%d" && printf %s "$FIXTURE_DATE"; }\n')
        with tempfile.TemporaryDirectory() as temp:
            output, environment = Path(temp, 'output'), Path(temp, 'environment')
            for day, attempt, expected in [('20261116', '1', 'v0.1.0-nightly.20261116.123'),
                                           ('20261116', '2', 'v0.1.0-nightly.20261116.123'),
                                           ('20261117', '3', 'v0.1.0-nightly.20261117.123')]:
                with self.subTest(day=day, attempt=attempt):
                    output.write_text('')
                    subprocess.run([bash, '-c', fixtures + script], check=True, capture_output=True,
                                   env={**os.environ, 'PUBLIC_SOURCE': 'true', 'FIXTURE_DATE': day,
                                        'RELEASE_PUBLISH_GHCR': 'true', 'RELEASE_PUBLISH_DOCKERHUB': 'false',
                                        'GITHUB_RUN_NUMBER': '123', 'GITHUB_RUN_ATTEMPT': attempt,
                                        'GITHUB_OUTPUT': output.as_posix(), 'GITHUB_ENV': environment.as_posix()})
                    rendered = output.read_text()
                    self.assertIn('version=' + expected + '\n', rendered)
                    self.assertIn('publish_ghcr=true\n', rendered)
                    self.assertIn('publish_dockerhub=false\n', rendered)
            for flag in ['RELEASE_PUBLISH_GHCR', 'RELEASE_PUBLISH_DOCKERHUB']:
                for bad in ['', 'yes', 'TRUE', 'FALSE']:
                    with self.subTest(flag=flag, bad=bad):
                        env = {**os.environ, 'PUBLIC_SOURCE': 'true', 'FIXTURE_DATE': '20261116',
                               'RELEASE_PUBLISH_GHCR': 'true', 'RELEASE_PUBLISH_DOCKERHUB': 'true',
                               'GITHUB_RUN_NUMBER': '123', 'GITHUB_RUN_ATTEMPT': '1',
                               'GITHUB_OUTPUT': output.as_posix(), 'GITHUB_ENV': environment.as_posix()}
                        if bad:
                            env[flag] = bad
                        else:
                            env.pop(flag, None)
                        result = subprocess.run([bash, '-c', fixtures + script], capture_output=True, text=True, env=env)
                        self.assertNotEqual(result.returncode, 0)

    def test_production_feature_flags_and_conditional_credentials_fail_closed(self):
        workflow = Path(__file__).resolve().parents[1] / '.github/workflows/release-production.yml'
        step = workflow.read_text().split('        id: identity\n', 1)[1]
        script = textwrap.dedent(step.split('        run: |\n', 1)[1].split('      - uses:', 1)[0])
        pwsh = shutil.which('pwsh')
        if not pwsh:
            self.skipTest('pwsh is not available')
        flags = [
            'RELEASE_SIGN_WINDOWS', 'RELEASE_PUBLISH_GHCR', 'RELEASE_PUBLISH_DOCKERHUB',
            'RELEASE_PUBLISH_SCOOP', 'RELEASE_PUBLISH_AUR', 'RELEASE_PUBLISH_WINGET',
            'RELEASE_PUBLISH_CHOCOLATEY',
        ]
        fixtures = ("function git { if ($args[0] -eq 'rev-parse') { '0' * 40 }; $global:LASTEXITCODE = 0 }\n")
        base_env = {
            **os.environ,
            'TAG': 'v0.1.0',
            'PUBLIC_SOURCE': 'true',
            'PACKAGE_LICENSE': 'AGPL-3.0-only',
            'MINISIGN_PUBLIC_KEY': 'pubkey',
            'MINISIGN_PRIVATE_KEY': 'privkey',
            'MINISIGN_PASSWORD': 'password',
            **{flag: 'false' for flag in flags},
        }
        for secret in [
            'SIGNPATH_API_TOKEN', 'SIGNPATH_ORGANIZATION_ID', 'SIGNPATH_PROJECT_SLUG',
            'SIGNPATH_SIGNING_POLICY_SLUG', 'SIGNPATH_ARTIFACT_CONFIGURATION_SLUG',
            'SCOOP_BUCKET_TOKEN', 'SCOOP_BUCKET_REPOSITORY', 'AUR_SSH_PRIVATE_KEY',
            'DOCKERHUB_USERNAME', 'DOCKERHUB_TOKEN', 'WINGET_TOKEN', 'CHOCOLATEY_API_KEY',
        ]:
            base_env.pop(secret, None)

        with tempfile.TemporaryDirectory() as temp:
            script_path = Path(temp, 'validate.ps1')
            script_path.write_text(fixtures + script, encoding='utf-8')
            output, environment = Path(temp, 'output'), Path(temp, 'env')

            def run_case(overrides):
                output.write_text('', encoding='utf-8')
                environment.write_text('', encoding='utf-8')
                env = {**base_env, 'GITHUB_OUTPUT': str(output), 'GITHUB_ENV': str(environment)}
                for key, value in overrides.items():
                    if value is None:
                        env.pop(key, None)
                    else:
                        env[key] = value
                return subprocess.run([pwsh, '-NoProfile', '-File', str(script_path)], capture_output=True, text=True, env=env)

            result = run_case({})
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn('sign_windows=false\n', output.read_text(encoding='utf-8'))

            for flag in flags:
                for bad in [None, 'yes', 'TRUE', 'FALSE']:
                    with self.subTest(flag=flag, bad=bad):
                        self.assertNotEqual(run_case({flag: bad}).returncode, 0)

            requirements = {
                'RELEASE_SIGN_WINDOWS': {
                    'SIGNPATH_API_TOKEN': 'tok',
                    'SIGNPATH_ORGANIZATION_ID': 'org',
                    'SIGNPATH_PROJECT_SLUG': 'proj',
                    'SIGNPATH_SIGNING_POLICY_SLUG': 'pol',
                    'SIGNPATH_ARTIFACT_CONFIGURATION_SLUG': 'art',
                },
                'RELEASE_PUBLISH_SCOOP': {
                    'SCOOP_BUCKET_TOKEN': 'tok',
                    'SCOOP_BUCKET_REPOSITORY': 'owner/bucket',
                },
                'RELEASE_PUBLISH_AUR': {'AUR_SSH_PRIVATE_KEY': 'key'},
                'RELEASE_PUBLISH_DOCKERHUB': {'DOCKERHUB_USERNAME': 'user', 'DOCKERHUB_TOKEN': 'tok'},
                'RELEASE_PUBLISH_WINGET': {'WINGET_TOKEN': 'tok'},
                'RELEASE_PUBLISH_CHOCOLATEY': {'CHOCOLATEY_API_KEY': 'key'},
            }
            for flag, creds in requirements.items():
                with self.subTest(enabled=flag):
                    self.assertEqual(run_case({flag: 'true', **creds}).returncode, 0)
                    for missing in creds:
                        partial = {k: v for k, v in creds.items() if k != missing}
                        self.assertNotEqual(run_case({flag: 'true', **partial}).returncode, 0)

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
                    if any('/releases/tags/nightly-stage-' in value for value in args):
                        raise AssertionError('pending drafts are not available through the published-tag endpoint')
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
                        value = [[previous.copy()], [replacement.copy()]]
                    elif any('/git/matching-refs/' in value for value in args):
                        value = [{'ref': 'refs/tags/' + name, 'object': {'sha': sha}} for name, sha in refs.items()]
                    else:
                        value = {'id': 42}
                    return subprocess.CompletedProcess(args, 0, stdout=json.dumps(value))
                if failure:
                    with self.assertRaises(subprocess.CalledProcessError):
                        self.module().publish_nightly(directory, 'v0.1.0-nightly.20261116.123', 'a'*40, runner)
                else:
                    self.module().publish_nightly(directory, 'v0.1.0-nightly.20261116.123', 'a'*40, runner)
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
            if any('/releases/tags/nightly-stage-' in value for value in args):
                raise AssertionError('pending drafts are not published tags')
            stage = next((call[3] for call in calls if call[:3] == ['gh', 'release', 'create']), None)
            value = [[{'id': 42, 'tag_name': stage, 'draft': True}]] if '--paginate' in args else []
            return subprocess.CompletedProcess(args, 0, stdout=json.dumps(value))
        with tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
            directory = Path(temp)
            (directory / 'checksums.txt').write_text('0'*64 + '  release.json\n')
            self.module().publish_nightly(directory, 'v0.1.0-nightly.20261116.123', 'a'*40, runner)
        upload = next(args for args in calls if args[:3] == ['gh', 'release', 'create'])
        self.assertEqual(upload[upload.index('--title') + 1], 'Nightly 2026-11-16')
        self.assertIn('/archive/' + 'a'*40 + '.tar.gz', upload[upload.index('--notes') + 1])
        create = next(index for index, args in enumerate(calls) if 'ref=refs/tags/nightly' in args)
        publish = next(index for index, args in enumerate(calls) if 'draft=false' in args)
        self.assertLess(create, publish)
        self.assertFalse(any(args[:3] == ['gh', 'release', 'delete'] for args in calls))

    def test_unexpected_draft_discovery_cannot_mutate_the_previous_nightly(self):
        for matches, draft in [(0, True), (2, True), (1, False)]:
            with self.subTest(matches=matches, draft=draft), tempfile.TemporaryDirectory(dir=Path(__file__).parent) as temp:
                directory = Path(temp)
                (directory / 'checksums.txt').write_text('0'*64 + '  release.json\n')
                calls = []
                def runner(args, **kwargs):
                    calls.append(args)
                    stage = next((call[3] for call in calls if call[:3] == ['gh', 'release', 'create']), None)
                    value = [[{'id': 42, 'tag_name': stage, 'draft': draft}] * matches]
                    return subprocess.CompletedProcess(args, 0, stdout=json.dumps(value))
                with self.assertRaises(ValueError):
                    self.module().publish_nightly(directory, 'v0.1.0-nightly.20261116.123', 'a'*40, runner)
                self.assertFalse(any('PATCH' in call or 'POST' in call or 'DELETE' in call for call in calls))

    def test_nightly_skip_uses_signed_manifest_commit(self):
        module = self.module()
        self.assertFalse(module.changed('a'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('b'*40, {'commit': 'a'*40, 'channel': 'develop'}))
        self.assertTrue(module.changed('a'*40, None))
        with self.assertRaises(ValueError):
            module.changed('a'*40, {'commit': 'a'*40, 'channel': 'main'})

    def git(self, *args):
        result = subprocess.run(['git', '-c', 'commit.gpgsign=false', '-c', 'core.hooksPath=',
                                 '-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid',
                                 *args], check=True, capture_output=True, text=True)
        return result.stdout.strip()

    def commit(self):
        self.git('add', '--force', '-A')
        self.git('commit', '--quiet', '-m', 'fixture')
        return self.git('rev-parse', 'HEAD')

    def test_nightly_compares_input_trees_against_the_published_baseline(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temp, chdir(temp):
            self.git('init', '--quiet')
            Path('app.go').write_text('package app\n')
            Path('README.md').write_text('original docs')
            baseline = self.commit()
            manifest = {'commit': baseline, 'channel': 'develop'}
            Path('README.md').write_text('updated docs')
            self.assertFalse(module.changed(self.commit(), manifest))
            Path('app.go').write_text('package changed\n')
            self.assertTrue(module.changed(self.commit(), manifest))
            Path('README.md').write_text('more docs after an application change')
            self.assertTrue(module.changed(self.commit(), manifest))
            Path('app.go').write_text('package app\n')
            self.assertFalse(module.changed(self.commit(), manifest))
            self.assertTrue(module.changed(self.git('rev-parse', 'HEAD'), {'commit': 'f'*40, 'channel': 'develop'}))

    def test_nightly_classifies_added_modified_and_deleted_build_inputs(self):
        module = self.module()
        cases = {
            'README.md': False, 'CHANGELOG.md': False, 'docs/release-contract.md': False, 'AGENTS.md': False,
            '.agents/skills/example/SKILL.md': False, 'internal/server/AGENTS.md': False,
            'internal/server/api_test.go': False, 'internal/server/testdata/response.json': False,
            'internal/server/testdata/runtime.spec.go': False,
            'web/src/App.test.ts': False, 'web/src/App.spec.ts': False,
            'packaging/release_test.py': False, 'packaging/production.test.cjs': False,
            'packaging/test-installer.ps1': False, 'packaging/README.md': False,
            '.github/workflows/release-verify.yml': False,
            'legal.go': True, 'cmd/anilist-arr-sync/main.go': True,
            'runtime.spec.go': True, 'cmd/anilist-arr-sync/runtime.spec.go': True,
            'internal/server/runtime.test.go': True, 'internal/server/test-runtime.go': True,
            'internal/server/api.go': True, 'internal/storage/migrations/001.sql': True,
            'web/src/App.svelte': True, 'web/src/theme.css': True,
            'web/public/help.md': True, 'web/public/example.test.json': True,
            'web/dist/help.md': True, 'web/embed.go': True,
            'go.mod': True, 'go.sum': True, 'web/package.json': True,
            'web/package-lock.json': True, 'web/vite.config.ts': True,
            'LICENSE': True, 'THIRD_PARTY_NOTICES.txt': True,
            '.goreleaser.yaml': True, 'Dockerfile': True,
            'packaging/windows.iss': True, 'packaging/windows.ps1': True,
            'packaging/resources.ps1': True, 'packaging/release.py': True,
            'packaging/container-config/README.md': True,
            '.github/actions/release-tools/action.yml': True,
            '.github/workflows/release-nightly.yml': True,
            '.github/workflows/release-containers.yml': True,
        }
        with tempfile.TemporaryDirectory() as temp, chdir(temp):
            self.git('init', '--quiet')
            self.git('commit', '--quiet', '--allow-empty', '-m', 'baseline')
            for name, expected in cases.items():
                with self.subTest(path=name):
                    baseline = self.git('rev-parse', 'HEAD')
                    path = Path(name)
                    path.parent.mkdir(parents=True, exist_ok=True)
                    path.write_text('original')
                    added = self.commit()
                    self.assertEqual(module.changed(added, {'commit': baseline, 'channel': 'develop'}), expected, 'addition')
                    path.write_text('modified')
                    modified = self.commit()
                    self.assertEqual(module.changed(modified, {'commit': added, 'channel': 'develop'}), expected, 'modification')
                    path.unlink()
                    deleted = self.commit()
                    self.assertEqual(module.changed(deleted, {'commit': modified, 'channel': 'develop'}), expected, 'deletion')

    def test_nightly_renames_check_both_source_and_destination(self):
        module = self.module()
        with tempfile.TemporaryDirectory() as temp, chdir(temp):
            self.git('init', '--quiet')
            Path('internal').mkdir()
            Path('docs').mkdir()
            Path('internal/app.go').write_text('package app\n')
            baseline = self.commit()
            self.git('mv', 'internal/app.go', 'docs/example.md')
            moved_out = self.commit()
            self.assertTrue(module.changed(moved_out, {'commit': baseline, 'channel': 'develop'}))
            self.git('mv', 'docs/example.md', 'internal/app.go')
            moved_in = self.commit()
            self.assertTrue(module.changed(moved_in, {'commit': moved_out, 'channel': 'develop'}))
            self.assertFalse(module.changed(moved_in, {'commit': baseline, 'channel': 'develop'}))

    def test_unchanged_nightly_with_missing_installer_is_rebuilt(self):
        module = self.module()
        metadata = {
            'release.json': json.dumps({'version': 'v0.1.0-nightly.20261116.123', 'commit': 'a'*40, 'channel': 'develop'}).encode(),
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
        with patch.object(module, 'api', side_effect=api), patch.object(module, 'urlopen', side_effect=download), patch.object(module, 'run', return_value=subprocess.CompletedProcess([], 0, stdout='README.md\0')):
            self.assertTrue(module.nightly_guard('b'*40), 'unchanged application inputs cannot hide missing release assets')

    def test_missing_nightly_metadata_is_rebuilt_without_trusting_it(self):
        names = ['release.json', 'checksums.txt', 'checksums.txt.minisig']
        for missing in names:
            for failure in ['absent', 404, 403]:
                with self.subTest(missing=missing, failure=failure):
                    module = self.module()
                    release = {'draft': False, 'prerelease': True, 'assets': [
                        {'name': name, 'browser_download_url': 'https://github.com/' + module.REPOSITORY + '/releases/download/nightly/' + name}
                        for name in names if failure != 'absent' or name != missing]}
                    def download(url, **kwargs):
                        if url.endswith('/' + missing):
                            error = HTTPError(url, failure, 'fixture download failure', {}, BytesIO())
                            self.addCleanup(error.close)
                            raise error
                        return BytesIO(b'fixture metadata')
                    with patch.object(module, 'api', return_value=release), patch.object(module, 'urlopen', side_effect=download), patch.object(module, 'run') as verifier:
                        if failure == 403:
                            with self.assertRaises(HTTPError): module.nightly_guard('a'*40)
                        else:
                            self.assertTrue(module.nightly_guard('a'*40))
                        verifier.assert_not_called()

    def test_nightly_api_authentication_and_unexpected_failures_propagate(self):
        module = self.module()
        for code in [403, 500]:
            with self.subTest(code=code):
                error = HTTPError('https://api.github.com/fixture', code, 'fixture failure', {}, BytesIO())
                self.addCleanup(error.close)
                with patch.dict(os.environ, {'GH_TOKEN': 'fixture'}), patch.object(module, 'urlopen', side_effect=error):
                    with self.assertRaises(HTTPError):
                        module.nightly_guard('a'*40)

    def test_nightly_guard_verifies_final_bytes_and_fails_closed_on_bad_metadata(self):
        version = 'v0.1.0-nightly.20261116.123'
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
                    if args[0] == 'git':
                        return subprocess.CompletedProcess(args, 0, stdout='README.md\0')
                    if args[3] == 'verify-nightly' and failure == 'signature':
                        raise subprocess.CalledProcessError(1, args)
                    if args[3] == 'verify':
                        self.assertEqual(kwargs['env']['RELEASE_COMMIT'], 'a'*40)
                        if Path(args[4], installer).read_bytes() != b'fixture artifact':
                            raise subprocess.CalledProcessError(1, args)
                with patch.object(module, 'api', side_effect=api), patch.object(module, 'urlopen', side_effect=download), patch.object(module, 'run', side_effect=verify) as verifier:
                    if failure == 'signature':
                        with self.assertRaises(subprocess.CalledProcessError): module.nightly_guard('b'*40)
                    else:
                        self.assertEqual(module.nightly_guard('b'*40), failure is not None)
                    self.assertEqual(verifier.call_count, 1 if failure == 'signature' else 2 if failure == 'tag' else 3)

    def test_extract_changelog_returns_matching_version_and_trims_reference_links(self):
        module = self.module()
        changelog = textwrap.dedent("""\
            # Changelog

            ## [Unreleased]

            ## [v1.1.0] - 2026-12-01

            ### Fixed
            - Fixed retry backoff.

            ## [v1.0.0] - 2026-11-27

            ### Added
            - Initial release.

            [Unreleased]: https://github.com/lucasskywalker64/anilist-arr-sync/compare/v1.1.0...HEAD
            [v1.1.0]: https://github.com/lucasskywalker64/anilist-arr-sync/compare/v1.0.0...v1.1.0
            [v1.0.0]: https://github.com/lucasskywalker64/anilist-arr-sync/releases/tag/v1.0.0
        """)
        self.assertEqual(module.extract_changelog(changelog, 'v1.1.0'), '### Fixed\n- Fixed retry backoff.')
        self.assertEqual(module.extract_changelog(changelog, 'v1.0.0'), '### Added\n- Initial release.')
        self.assertEqual(module.extract_changelog('## v1.0.0\n\n- Plain heading.\n', 'v1.0.0'), '- Plain heading.')

    def test_extract_changelog_fails_closed_on_missing_empty_duplicate_or_prerelease_headings(self):
        module = self.module()
        cases = [
            ('missing', '# Changelog\n\n## [v1.0.0] - 2026-11-27\n\n- Init.\n', 'v2.0.0'),
            ('prerelease-mismatch', '# Changelog\n\n## [v1.0.0-rc.1] - 2026-11-27\n\n- RC.\n', 'v1.0.0'),
            ('empty', '# Changelog\n\n## [v1.0.0] - 2026-11-27\n\n## [v0.9.0]\n\n- Old.\n', 'v1.0.0'),
            ('only-links', '# Changelog\n\n## [v1.0.0]\n\n[v1.0.0]: https://example.com\n', 'v1.0.0'),
            ('duplicate', '# Changelog\n\n## [v1.0.0]\n\n- One.\n\n## [v1.0.0]\n\n- Two.\n', 'v1.0.0'),
        ]
        for label, text, version in cases:
            with self.subTest(case=label):
                with self.assertRaises(ValueError):
                    module.extract_changelog(text, version)

    def test_render_and_write_release_notes_combine_changelog_with_signing_and_policy_footer(self):
        module = self.module()
        commit = 'a' * 40
        with tempfile.TemporaryDirectory() as temp:
            changelog = Path(temp, 'CHANGELOG.md')
            output = Path(temp, '.release/notes.md')
            changelog.write_text('# Changelog\n\n## [v1.0.0] - 2026-11-27\n\n### Added\n- First release.\n', encoding='utf-8')
            for sign_flag, expected_signing in [
                ('true', 'Windows artifacts have timestamped SignPath signatures.'),
                ('false', 'Windows artifacts omit Authenticode signatures for this release.'),
            ]:
                with self.subTest(sign_windows=sign_flag):
                    env = {'RELEASE_VERSION': 'v1.0.0', 'RELEASE_COMMIT': commit, 'RELEASE_SIGN_WINDOWS': sign_flag}
                    with patch.dict(os.environ, env, clear=False):
                        module.write_release_notes(output_path=output, changelog_path=changelog)
                    rendered = output.read_text(encoding='utf-8')
                    self.assertTrue(rendered.startswith('### Added\n- First release.\n\n---\n\n'))
                    self.assertIn(expected_signing, rendered)
                    self.assertIn(f'/archive/{commit}.tar.gz', rendered)
                    self.assertIn(f'/blob/{commit}/CODE_SIGNING.md', rendered)
                    self.assertIn(f'/blob/{commit}/PRIVACY.md', rendered)
                    self.assertIn(f'/blob/{commit}/SECURITY.md', rendered)

            with patch.dict(os.environ, {'RELEASE_VERSION': 'v1.0.0', 'RELEASE_COMMIT': commit, 'RELEASE_SIGN_WINDOWS': 'yes'}, clear=False):
                with self.assertRaises(ValueError):
                    module.write_release_notes(output_path=output, changelog_path=changelog)

            with self.assertRaises(ValueError):
                module.render_release_notes('v1.0.0', commit, False, changelog_path=Path(temp, 'missing.md'))


if __name__ == '__main__':
    unittest.main()
