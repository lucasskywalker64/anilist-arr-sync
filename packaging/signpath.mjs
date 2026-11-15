// GoReleaser and Inno hooks use the official trusted GitHub connector. Keeping
// this synchronous prevents packaging before the required approval completes.
import { DefaultArtifactClient } from '@actions/artifact';
import { execFileSync } from 'node:child_process';
import { copyFileSync, existsSync, mkdirSync } from 'node:fs';
import { basename, dirname, resolve } from 'node:path';
import { randomUUID } from 'node:crypto';

try {
  if (process.env.RELEASE_MODE !== 'production') throw new Error('SignPath requires explicit production mode');
  for (const name of ['SIGNPATH_API_TOKEN', 'SIGNPATH_ORGANIZATION_ID', 'SIGNPATH_PROJECT_SLUG', 'SIGNPATH_SIGNING_POLICY_SLUG', 'SIGNPATH_ARTIFACT_CONFIGURATION_SLUG', 'GITHUB_TOKEN', 'ACTIONS_RUNTIME_TOKEN']) {
    if (!process.env[name]) throw new Error(`Missing ${name}`);
  }
  if (process.argv.length !== 3) throw new Error('Expected one executable to sign');
  const file = resolve(process.argv[2]);
  const action = resolve('.release-tools/signpath/index.js');
  if (!existsSync(action)) throw new Error('Pinned official SignPath action is missing');
  const request = randomUUID();
  const output = resolve('.release/signpath', request);
  mkdirSync(output, { recursive: true });
  const { id } = await new DefaultArtifactClient().uploadArtifact(`sign-${request}`, [file], dirname(file), { retentionDays: 1 });
  if (!id) throw new Error('GitHub did not return an artifact ID');
  const inputs = {
    'api-token': process.env.SIGNPATH_API_TOKEN,
    'organization-id': process.env.SIGNPATH_ORGANIZATION_ID,
    'project-slug': process.env.SIGNPATH_PROJECT_SLUG,
    'signing-policy-slug': process.env.SIGNPATH_SIGNING_POLICY_SLUG,
    'artifact-configuration-slug': process.env.SIGNPATH_ARTIFACT_CONFIGURATION_SLUG,
    'github-artifact-id': String(id),
    'github-token': process.env.GITHUB_TOKEN,
    'connector-url': 'https://pipelineconnector.connectors.signpath.io/GitHubActions/GitHubCom',
    'wait-for-completion': 'true',
    'wait-for-completion-timeout-in-seconds': '3600',
    'service-unavailable-timeout-in-seconds': '600',
    'download-signed-artifact-timeout-in-seconds': '300',
    'output-artifact-directory': output,
    'skip-decompress': 'false',
  };
  const env = { ...process.env };
  for (const [name, value] of Object.entries(inputs)) env[`INPUT_${name.toUpperCase()}`] = value;
  execFileSync(process.execPath, [action], { env, stdio: 'inherit', timeout: 4_200_000 });
  const signed = resolve(output, basename(file));
  if (!existsSync(signed)) throw new Error('SignPath did not return the requested executable');
  execFileSync('pwsh', ['-NoProfile', '-File', resolve('packaging/verify-authenticode.ps1'), '-Path', signed], { stdio: 'inherit' });
  copyFileSync(signed, file);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}
