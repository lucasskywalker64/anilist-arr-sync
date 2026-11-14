// The runner supplies Actions artifact credentials to JavaScript actions.
// Child signing hooks inherit that scoped runtime; shell steps do not receive it.
const { spawnSync } = require('node:child_process');

for (const [command, args] of [
  ['go', ['run', './cmd/release', 'config']],
  ['goreleaser', ['release', '--config', '.release-tools/goreleaser.yaml', '--clean', '--fail-fast', '--parallelism=1', '--timeout=5h', '--skip=docker', '--release-notes', '.release/notes.md']],
  ['go', ['run', './cmd/release', 'export', 'dist']],
]) {
  const result = spawnSync(command, args, { stdio: 'inherit', env: process.env });
  if (result.error || result.status !== 0) {
    console.error(`${command} failed; release stopped without an unsigned fallback`);
    process.exit(result.status || 1);
  }
}
