const { test } = require('node:test');
const assert = require('node:assert/strict');
const child = require('node:child_process');
const path = require('node:path');

const action = path.resolve(__dirname, '../.github/actions/production-release/index.cjs');

for (const failedCommand of [null, 0, 1, 2]) {
  test(`production command boundary stops at ${failedCommand ?? 'success'}`, (t) => {
    const calls = [];
    t.mock.method(child, 'spawnSync', (command, args, options) => {
      assert.ok(options.env === process.env, 'child hooks must inherit the JavaScript action runtime');
      calls.push([command, ...args]);
      return { status: calls.length - 1 === failedCommand ? 1 : 0 };
    });
    t.mock.method(console, 'error', () => {});
    t.mock.method(process, 'exit', () => { throw new Error('fixture exit'); });
    delete require.cache[action];
    if (failedCommand === null) {
      require(action);
      assert.equal(calls.length, 3);
      assert.equal(calls[1][0], 'goreleaser');
      assert.deepEqual(calls[2], ['go', 'run', './cmd/release', 'export', 'dist']);
    } else {
      assert.throws(() => require(action), /fixture exit/);
      assert.equal(calls.length, failedCommand + 1);
    }
  });
}
