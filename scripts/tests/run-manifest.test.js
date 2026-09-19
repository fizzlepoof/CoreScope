#!/usr/bin/env node
'use strict';

const assert = require('assert');
const fs = require('fs');
const path = require('path');
const {
  parseArguments,
  dispatchTests,
  selectTests,
  preflightTests,
  runTests,
} = require('./run-manifest');

let passed = 0;
let failed = 0;

function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  ✓ ${name}`);
  } catch (error) {
    failed++;
    console.error(`  ✗ ${name}: ${error.message}`);
  }
}

function entry(testPath, suite = 'unit', status = 'active', overrides = {}) {
  return {
    path: testPath,
    suite,
    status,
    command: ['node', testPath],
    requirements: { environment: [], flags: [], packages: [] },
    ...overrides,
  };
}

test('parses deterministic profile, suite, status, list, and dry-run options', () => {
  assert.deepStrictEqual(parseArguments([]), {
    profile: null,
    suites: ['e2e', 'integration', 'unit'],
    statuses: ['active'],
    list: false,
    dryRun: false,
  });
  assert.deepStrictEqual(
    parseArguments(
      ['--profile', 'ci-unit-and-integration-phase', '--suite', 'unit,e2e', '--status=dormant', '--list', '--dry-run'],
      ['ci-unit-and-integration-phase']
    ),
    {
      profile: 'ci-unit-and-integration-phase',
      suites: ['e2e', 'unit'],
      statuses: ['dormant'],
      list: true,
      dryRun: true,
    }
  );
  assert.throws(() => parseArguments(['--suite', 'banana']), /invalid suite: banana/);
  assert.throws(() => parseArguments(['--status']), /--status requires a value/);
  assert.throws(
    () => parseArguments(['--profile=stale'], ['ci-unit-and-integration-phase']),
    /invalid profile: stale/
  );
  assert.throws(
    () => parseArguments(
      ['--profile=ci-unit-and-integration-phase', '--profile', 'ci-unit-and-integration-phase'],
      ['ci-unit-and-integration-phase']
    ),
    /--profile may only be specified once/
  );
  assert.throws(() => parseArguments(['--unknown']), /unknown argument: --unknown/);
});

test('combines a profile with suite and status filters in path order', () => {
  const manifest = {
    tests: [
      entry('test-z.js', 'unit'),
      entry('test-dormant.js', 'unit', 'dormant'),
      entry('test-a.js', 'e2e'),
      entry('test-middle.js', 'integration'),
      entry('test-extra.js', 'unit'),
      entry('test-wrapper.sh', 'unit', 'active', { orchestration: true }),
    ],
  };
  const profiles = {
    selected: ['test-z.js', 'test-a.js'],
  };
  assert.deepStrictEqual(
    selectTests(manifest, {
      profile: 'selected',
      suites: ['unit', 'e2e'],
      statuses: ['active'],
    }, profiles).map(item => item.path),
    ['test-a.js', 'test-z.js']
  );
});

test('rejects malformed profiles instead of silently dropping entries', () => {
  const active = entry('test-a.js');
  const dormant = entry('test-dormant.js', 'unit', 'dormant');
  const orchestration = entry('test-all.sh', 'integration', 'active', {
    command: ['sh', 'test-all.sh'],
    orchestration: true,
  });
  const manifest = { tests: [active, dormant, orchestration] };
  const options = {
    profile: 'broken',
    suites: ['unit', 'integration'],
    statuses: ['active'],
  };
  assert.throws(
    () => selectTests(manifest, options, { broken: ['test-a.js', 'test-typo.js'] }),
    /references missing test: test-typo\.js/
  );
  assert.throws(
    () => selectTests(manifest, options, { broken: ['test-a.js', 'test-a.js'] }),
    /contains duplicate paths/
  );
  assert.throws(
    () => selectTests(manifest, options, { broken: ['test-dormant.js'] }),
    /references non-active test/
  );
  assert.throws(
    () => selectTests(manifest, options, { broken: ['test-all.sh'] }),
    /references orchestration test/
  );
});

test('selects tests by suite and status in path order', () => {
  const manifest = {
    tests: [
      entry('test-z.js', 'unit'),
      entry('test-dormant.js', 'unit', 'dormant'),
      entry('test-a.js', 'e2e'),
      entry('test-middle.js', 'integration'),
    ],
  };
  assert.deepStrictEqual(
    selectTests(manifest, { suites: ['unit', 'e2e'], statuses: ['active'] }).map(item => item.path),
    ['test-a.js', 'test-z.js']
  );
  assert.deepStrictEqual(
    selectTests(manifest, { suites: ['unit'], statuses: ['dormant'] }).map(item => item.path),
    ['test-dormant.js']
  );
});

test('list and dry-run inventory modes do not require execution dependencies', () => {
  const tests = [entry('test-needs.js', 'e2e', 'dormant', {
    requirements: {
      environment: [{ name: 'SERVER_URL', required: true }],
      flags: [],
      packages: [{ name: 'missing-package', required: true }],
    },
  })];
  for (const mode of [{ list: true, dryRun: false }, { list: false, dryRun: true }]) {
    const output = [];
    const status = dispatchTests(tests, mode, {
      preflight: () => { throw new Error('must not preflight inventory mode'); },
      run: () => { throw new Error('must not execute inventory mode'); },
      writeOutput: line => output.push(line),
    });
    assert.strictEqual(status, 0);
    assert(output.join('').includes('test-needs.js'));
  }
});

test('excludes orchestration entries from child execution', () => {
  const manifest = {
    tests: [
      entry('test-all.sh', 'integration', 'active', {
        command: ['sh', 'test-all.sh'],
        orchestration: true,
      }),
      entry('test-real.js'),
    ],
  };
  assert.deepStrictEqual(
    selectTests(manifest, { suites: ['unit', 'integration'], statuses: ['active'] }).map(item => item.path),
    ['test-real.js']
  );
});

test('preflight reports all missing required environment and packages before execution', () => {
  const tests = [entry('test-needs.js', 'integration', 'active', {
    requirements: {
      environment: [{ name: 'SERVICE_URL', required: true }],
      flags: [],
      packages: [{ name: 'missing-package', required: true }],
    },
  })];
  assert.throws(
    () => preflightTests(tests, {
      repoRoot: '/repo',
      environment: {},
      resolvePackage: () => { throw new Error('not found'); },
    }),
    error => /SERVICE_URL/.test(error.message) && /missing-package/.test(error.message)
  );
});

test('executes exact argv from repository root with inherited env and flag defaults', () => {
  const calls = [];
  const tests = [entry('test-exact.js', 'unit', 'active', {
    command: ['node', 'test-exact.js'],
    requirements: {
      environment: [],
      flags: [{ name: 'STRICT_MODE', value: '1' }],
      packages: [],
    },
  })];
  const result = runTests(tests, {
    repoRoot: '/repo',
    environment: { KEEP_ME: 'yes', STRICT_MODE: '0' },
    spawnSync: (executable, argv, options) => {
      calls.push({ executable, argv, options });
      return { status: 0 };
    },
  });
  assert.strictEqual(result, 0);
  assert.deepStrictEqual(calls[0].executable, 'node');
  assert.deepStrictEqual(calls[0].argv, ['test-exact.js']);
  assert.strictEqual(calls[0].options.cwd, path.resolve('/repo'));
  assert.strictEqual(calls[0].options.shell, false);
  assert.strictEqual(calls[0].options.stdio, 'inherit');
  assert.strictEqual(calls[0].options.env.KEEP_ME, 'yes');
  assert.strictEqual(calls[0].options.env.STRICT_MODE, '1');
});

test('stops at the first failure and propagates its exit status', () => {
  const executed = [];
  const status = runTests([entry('test-a.js'), entry('test-b.js')], {
    repoRoot: '/repo',
    environment: {},
    spawnSync: (_executable, argv) => {
      executed.push(argv[0]);
      return { status: 7 };
    },
  });
  assert.strictEqual(status, 7);
  assert.deepStrictEqual(executed, ['test-a.js']);
});

test('reports spawn errors as failure without running later tests', () => {
  const executed = [];
  const status = runTests([entry('test-a.js'), entry('test-b.js')], {
    repoRoot: '/repo',
    environment: {},
    writeError: () => {},
    spawnSync: (_executable, argv) => {
      executed.push(argv[0]);
      return { status: null, error: new Error('ENOENT') };
    },
  });
  assert.strictEqual(status, 1);
  assert.deepStrictEqual(executed, ['test-a.js']);
});

test('checked-in profiles exactly match each frozen legacy execution list', () => {
  const repoRoot = path.resolve(__dirname, '../..');
  const manifest = JSON.parse(fs.readFileSync(path.join(repoRoot, 'tests/manifest.json'), 'utf8'));
  const inventory = JSON.parse(
    fs.readFileSync(path.join(repoRoot, 'tests/legacy-runner-inventory.json'), 'utf8')
  );
  for (const [profile, expected] of Object.entries(inventory.executedRootTests)) {
    const selected = selectTests(manifest, {
      profile,
      suites: ['unit', 'integration', 'e2e'],
      statuses: ['active'],
    }, inventory.executedRootTests).map(item => item.path);
    assert.deepStrictEqual(selected, [...expected].sort(), profile);
    assert(!selected.some(testPath =>
      manifest.tests.some(item => item.path === testPath && item.status === 'dormant')
    ));
  }

  const wrapper = manifest.tests.find(item => item.path === 'test-all.sh');
  assert(wrapper, 'test-all.sh must remain represented in the manifest');
  assert.strictEqual(wrapper.orchestration, true);
  assert(!Object.values(inventory.executedRootTests).flat().includes('test-all.sh'));
});

test('local and CI entry points name their exact legacy profiles', () => {
  const repoRoot = path.resolve(__dirname, '../..');
  const packageJson = JSON.parse(fs.readFileSync(path.join(repoRoot, 'package.json'), 'utf8'));
  const testAll = fs.readFileSync(path.join(repoRoot, 'test-all.sh'), 'utf8');
  const workflow = fs.readFileSync(path.join(repoRoot, '.github/workflows/deploy.yml'), 'utf8');

  assert(packageJson.scripts['test:unit'].includes('--profile legacy-test-unit'));
  assert(packageJson.scripts['test:ci'].includes('--profile ci-unit-and-integration-phase'));
  assert(packageJson.scripts['test:ci:e2e'].includes('--profile ci-e2e-phase'));
  assert(testAll.includes('--profile local-package-and-test-all'));
  assert(workflow.includes('npm run test:ci'));
  assert(workflow.includes('npm run test:ci:e2e'));
});

console.log(`\n${passed} passed, ${failed} failed`);
process.exit(failed ? 1 : 0);
