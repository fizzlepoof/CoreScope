#!/usr/bin/env node
'use strict';

const assert = require('assert');
const crypto = require('crypto');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { spawnSync } = require('child_process');

const repoRoot = path.resolve(__dirname, '../..');
const script = path.join(repoRoot, 'scripts/combined-coverage.sh');
const library = path.join(repoRoot, 'scripts/combined-coverage-lib.sh');

function sourceScript(lines, environment = {}) {
  return spawnSync('sh', ['-c', [
    'set -eu',
    `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(repoRoot)}`,
    'export COMBINED_COVERAGE_REPO_ROOT',
    `. ${JSON.stringify(library)}`,
    ...lines,
  ].join('\n')], {
    cwd: repoRoot,
    env: { ...process.env, COMBINED_COVERAGE_REPO_ROOT: repoRoot, ...environment },
    encoding: 'utf8',
  });
}

function run(args, environment = {}) {
  return spawnSync('sh', [script, ...args], {
    cwd: repoRoot,
    env: { ...process.env, ...environment },
    encoding: 'utf8',
  });
}

function sha256(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}

let passed = 0;
function test(name, fn) {
  try {
    fn();
    passed++;
    console.log(`  PASS ${name}`);
  } catch (error) {
    console.error(`  FAIL ${name}: ${error.message}`);
    process.exitCode = 1;
  }
}

test('--help documents supported coverage modes', () => {
  const result = run(['--help']);
  assert.strictEqual(result.status, 0, result.stderr);
  assert.match(result.stdout, /--go-only/);
  assert.match(result.stdout, /--frontend-only/);
  assert.match(result.stdout, /--dry-run/);
  assert.match(result.stdout, /COVERAGE_PORT/);
});

test('--dry-run describes the current Go and canonical frontend flow without mutation', () => {
  const fixture = path.join(repoRoot, 'test-fixtures/e2e-fixture.db');
  const before = sha256(fixture);
  const result = run(['--dry-run'], { COVERAGE_PORT: '24680' });
  assert.strictEqual(result.status, 0, result.stderr);
  const output = `${result.stdout}\n${result.stderr}`;
  assert.match(output, /cmd\/server.*go test.*-coverprofile/);
  assert.match(output, /cmd\/ingestor.*go test.*-coverprofile/);
  assert.match(output, /cmd\/server.*go build/);
  assert.match(output, /cmd\/migrate.*go build/);
  assert.match(output, /copy.*test-fixtures\/e2e-fixture\.db/i);
  assert.match(output, /corescope-migrate.*-db/);
  assert.match(output, /-host 127\.0\.0\.1/);
  assert.match(output, /-port 24680/);
  assert.match(output, /-config-dir <temporary>\/config/);
  assert.match(output, /--profile ci-e2e-phase/);
  assert.match(output, /collect-frontend-coverage\.js/);
  assert.match(output, /nyc report/);
  assert.strictEqual(sha256(fixture), before, 'dry-run mutated the tracked fixture');
});

test('invalid configured ports fail before orchestration', () => {
  const result = run(['--dry-run'], { COVERAGE_PORT: 'not-a-port' });
  assert.notStrictEqual(result.status, 0);
  assert.match(result.stderr, /COVERAGE_PORT/);
});

test('inherited source-only variables cannot bypass direct CLI execution', () => {
  for (const variable of ['COMBINED_COVERAGE_SOURCE_ONLY', 'COMBINED_COVERAGE_TEST_SOURCE_ONLY']) {
    const result = run(['--not-a-real-option'], { [variable]: '1' });
    assert.strictEqual(result.status, 2, `${variable}: ${result.stderr || result.stdout}`);
    assert.match(result.stderr, /unknown option/);
  }
});

test('renamed and symlinked entry points still execute the CLI wrapper', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-entry-'));
  const renamed = path.join(temp, 'coverage-entry');
  fs.symlinkSync(script, renamed);
  try {
    const result = spawnSync('sh', [renamed, '--not-a-real-option'], {
      cwd: repoRoot,
      env: process.env,
      encoding: 'utf8',
    });
    assert.strictEqual(result.status, 2, result.stderr || result.stdout);
    assert.match(result.stderr, /unknown option/);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('Playwright launchers allow bundled Chromium when CHROMIUM_PATH is unset', () => {
  const offenders = fs.readdirSync(repoRoot)
    .filter(name => /^test-.*\.js$/.test(name))
    .filter(name => /process\.env\.CHROMIUM_PATH\s*\|\|\s*['"]\/usr\/bin\/chromium['"]/.test(
      fs.readFileSync(path.join(repoRoot, name), 'utf8'),
    ));
  assert.deepStrictEqual(offenders, [], `hard-coded Chromium fallbacks: ${offenders.join(', ')}`);
});

test('temporary fixture seeding adds a current deterministic multi-hop packet', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-fixture-'));
  const fixture = path.join(temp, 'fixture.db');
  fs.copyFileSync(path.join(repoRoot, 'test-fixtures/e2e-fixture.db'), fixture);
  try {
    const addMigratedColumns = spawnSync('python3', ['-c', [
      'import sqlite3, sys',
      'db=sqlite3.connect(sys.argv[1])',
      "db.execute('ALTER TABLE transmissions ADD COLUMN last_seen INTEGER NOT NULL DEFAULT 0')",
      "db.execute('ALTER TABLE observations ADD COLUMN raw_hex TEXT')",
      'db.commit()',
    ].join('\n'), fixture], { encoding: 'utf8' });
    assert.strictEqual(addMigratedColumns.status, 0, addMigratedColumns.stderr);
    const seedCommand = [
      'set -eu',
        `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(repoRoot)}`,
      'export COMBINED_COVERAGE_REPO_ROOT',
      `. ${JSON.stringify(library)}`,
      `seed_e2e_fixture ${JSON.stringify(fixture)}`,
      `finalize_e2e_fixture ${JSON.stringify(fixture)}`,
    ].join('\n');
    const seeded = spawnSync('sh', ['-c', seedCommand], { cwd: repoRoot, encoding: 'utf8' });
    assert.strictEqual(seeded.status, 0, seeded.stderr);
    const query = [
      'import sqlite3, sys',
      'db=sqlite3.connect(sys.argv[1])',
      "rows=db.execute(\"SELECT t.raw_hex, json_array_length(o.path_json), t.hash, t.last_seen, o.raw_hex, r.iata FROM transmissions t JOIN observations o ON o.transmission_id=t.id LEFT JOIN observers r ON r.rowid=o.observer_idx WHERE t.id BETWEEN 1000000 AND 1000009 ORDER BY t.id\").fetchall()",
      'print(rows)',
      "assert len(rows) == 10 and len({row[0] for row in rows}) == 10 and len({row[2] for row in rows}) == 10 and all(row[0].lower().startswith('11467d1dda2aeacf1000ec344000') and row[1] == 6 and len(row[2]) == 16 and all(c in '0123456789abcdef' for c in row[2].lower()) and row[3] > 0 and row[4] == row[0] and row[5] and len(row[5]) == 3 for row in rows)",
    ].join('\n');
    const checked = spawnSync('python3', ['-c', query, fixture], { encoding: 'utf8' });
    assert.strictEqual(checked.status, 0, checked.stderr || checked.stdout);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('frontend path preparation preserves pre-existing generated state', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-preserve-'));
  const work = path.join(tempRoot, 'work');
  const instrumented = path.join(tempRoot, 'public-instrumented');
  const nycOutput = path.join(tempRoot, '.nyc_output');
  const frontendReport = path.join(tempRoot, 'coverage', 'frontend');
  for (const directory of [work, instrumented, nycOutput, frontendReport]) fs.mkdirSync(directory, { recursive: true });
  for (const directory of [instrumented, nycOutput, frontendReport]) fs.writeFileSync(path.join(directory, 'sentinel.txt'), 'keep me');
  try {
    const command = [
      'set -eu',
        `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(tempRoot)}`,
      `COVERAGE_DIR=${JSON.stringify(path.join(tempRoot, 'coverage'))}`,
      'export COMBINED_COVERAGE_REPO_ROOT COVERAGE_DIR',
      `. ${JSON.stringify(library)}`,
      `WORK_DIR=${JSON.stringify(work)}`,
      'prepare_frontend_paths',
      '[ "$RUN_INSTRUMENTED_DIR" != "$INSTRUMENTED_DIR" ]',
      '[ "$RUN_FRONTEND_REPORT_DIR" != "$FRONTEND_COVERAGE_DIR" ]',
      'printf run > "$REPO_ROOT/.nyc_output/run.json"',
      'cleanup',
      '[ ! -e "$WORK_DIR" ]',
      '[ -f "$REPO_ROOT/.nyc_output/sentinel.txt" ]',
      '[ ! -e "$REPO_ROOT/.nyc_output/run.json" ]',
      '[ -f "$INSTRUMENTED_DIR/sentinel.txt" ]',
      '[ -f "$FRONTEND_COVERAGE_DIR/sentinel.txt" ]',
    ].join('\n');
    const result = spawnSync('sh', ['-c', command], { cwd: repoRoot, encoding: 'utf8' });
    assert.strictEqual(result.status, 0, result.stderr || result.stdout);
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

test('frontend path preparation restores pre-existing state when link creation fails', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-link-failure-'));
  const work = path.join(tempRoot, 'work');
  const fakeBin = path.join(tempRoot, 'bin');
  const nycOutput = path.join(tempRoot, '.nyc_output');
  fs.mkdirSync(work);
  fs.mkdirSync(fakeBin);
  fs.mkdirSync(nycOutput);
  fs.writeFileSync(path.join(nycOutput, 'sentinel.txt'), 'keep me');
  const fakeLn = path.join(fakeBin, 'ln');
  fs.writeFileSync(fakeLn, '#!/bin/sh\nexit 42\n', { mode: 0o755 });
  try {
    const command = [
      'set -eu',
        `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(tempRoot)}`,
      `COVERAGE_DIR=${JSON.stringify(path.join(tempRoot, 'coverage'))}`,
      'export COMBINED_COVERAGE_REPO_ROOT COVERAGE_DIR',
      `. ${JSON.stringify(library)}`,
      `WORK_DIR=${JSON.stringify(work)}`,
      'trap cleanup EXIT',
      'prepare_frontend_paths',
    ].join('\n');
    const result = spawnSync('sh', ['-c', command], {
      cwd: repoRoot,
      env: { ...process.env, PATH: `${fakeBin}:${process.env.PATH}` },
      encoding: 'utf8',
    });
    assert.strictEqual(result.status, 42, result.stderr || result.stdout);
    assert.strictEqual(fs.readFileSync(path.join(nycOutput, 'sentinel.txt'), 'utf8'), 'keep me');
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

test('frontend path preparation restores state when interrupted after link creation', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-link-signal-'));
  const work = path.join(tempRoot, 'work');
  const fakeBin = path.join(tempRoot, 'bin');
  const nycOutput = path.join(tempRoot, '.nyc_output');
  fs.mkdirSync(work);
  fs.mkdirSync(fakeBin);
  fs.mkdirSync(nycOutput);
  fs.writeFileSync(path.join(nycOutput, 'sentinel.txt'), 'keep me');
  const fakeLn = path.join(fakeBin, 'ln');
  fs.writeFileSync(fakeLn, '#!/bin/sh\n/bin/ln "$@"\nkill -TERM "$PPID"\nexit 0\n', { mode: 0o755 });
  try {
    const command = [
      'set -eu',
        `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(tempRoot)}`,
      `COVERAGE_DIR=${JSON.stringify(path.join(tempRoot, 'coverage'))}`,
      'export COMBINED_COVERAGE_REPO_ROOT COVERAGE_DIR',
      `. ${JSON.stringify(library)}`,
      `WORK_DIR=${JSON.stringify(work)}`,
      'trap cleanup EXIT',
      "trap 'exit 130' HUP INT TERM",
      'prepare_frontend_paths',
    ].join('\n');
    const result = spawnSync('sh', ['-c', command], {
      cwd: repoRoot,
      env: { ...process.env, PATH: `${fakeBin}:${process.env.PATH}` },
      encoding: 'utf8',
    });
    assert.strictEqual(result.status, 130, result.stderr || result.stdout);
    assert.strictEqual(fs.readFileSync(path.join(nycOutput, 'sentinel.txt'), 'utf8'), 'keep me');
    assert.strictEqual(fs.lstatSync(nycOutput).isDirectory(), true);
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

test('instrumentation refuses to overwrite a pre-existing target', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-instrument-preserve-'));
  const sentinel = path.join(temp, 'sentinel.txt');
  fs.writeFileSync(sentinel, 'keep me');
  try {
    const result = spawnSync('sh', ['scripts/instrument-frontend.sh'], {
      cwd: repoRoot,
      env: { ...process.env, INSTRUMENTED_DIR: temp },
      encoding: 'utf8',
    });
    assert.notStrictEqual(result.status, 0);
    assert.match(result.stderr, /already exists/);
    assert.strictEqual(fs.readFileSync(sentinel, 'utf8'), 'keep me');
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('frontend instrumentation remains compatible with the production CSP', () => {
  const tempRoot = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-instrument-csp-'));
  const target = path.join(tempRoot, 'public-instrumented');
  try {
    const result = spawnSync('sh', ['scripts/instrument-frontend.sh'], {
      cwd: repoRoot,
      env: { ...process.env, INSTRUMENTED_DIR: target },
      encoding: 'utf8',
    });
    assert.strictEqual(result.status, 0, result.stderr || result.stdout);
    const instrumented = fs.readFileSync(path.join(target, 'payload-labels.js'), 'utf8');
    assert.doesNotMatch(instrumented, /new Function\s*\(/,
      'Istanbul global lookup must not require unsafe-eval under the production CSP');
  } finally {
    fs.rmSync(tempRoot, { recursive: true, force: true });
  }
});

test('frontend collector rejects empty and rejected group results', () => {
  const collector = require('../collect-frontend-coverage.js');
  const complete = Array.from({ length: 7 }, (_, i) => ({
    status: 'fulfilled',
    value: { [`file-${i}.js`]: {} },
  }));
  assert.doesNotThrow(() => collector.validateCoverageResults(complete));
  const empty = complete.slice();
  empty[2] = { status: 'fulfilled', value: {} };
  assert.throws(() => collector.validateCoverageResults(empty), /group 3: no coverage/);
  const rejected = complete.slice();
  rejected[4] = { status: 'rejected', reason: new Error('browser failed') };
  assert.throws(() => collector.validateCoverageResults(rejected), /group 5: browser failed/);
});

test('frontend artifact validation requires canonical coherent measured files', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-artifacts-'));
  try {
    const source = path.join(temp, 'public');
    const instrumented = path.join(temp, 'public-instrumented');
    fs.mkdirSync(source);
    fs.mkdirSync(instrumented);
    const measuredFile = path.join(source, 'file.js');
    const measuredSource = 'function covered(value) { return value ? 1 : 0; }\n';
    fs.writeFileSync(measuredFile, measuredSource);
    fs.writeFileSync(path.join(instrumented, 'file.js'), measuredSource);
    const expected = ['e2e-coverage.json', ...Array.from({ length: 7 }, (_, i) => `frontend-coverage-g${i + 1}.json`)];
    const validReport = {
      [measuredFile]: {
        path: measuredFile,
        statementMap: { '0': { start: { line: 1, column: 0 }, end: { line: 1, column: 1 } } },
        fnMap: { '0': { name: 'covered', decl: { start: { line: 1, column: 0 }, end: { line: 1, column: 7 } }, loc: { start: { line: 1, column: 0 }, end: { line: 1, column: 50 } } } },
        branchMap: { '0': { type: 'if', locations: [{ start: { line: 1, column: 34 }, end: { line: 1, column: 35 } }, { start: {}, end: {} }] } },
        s: { '0': 1 }, f: { '0': 1 }, b: { '0': [1, 0] },
      },
    };
    const validCoverage = JSON.stringify(validReport);
    const reset = () => expected.forEach(name => fs.writeFileSync(path.join(temp, name), validCoverage));
    const validate = () => sourceScript([
      `REPO_ROOT=${JSON.stringify(temp)}`,
      `RUN_INSTRUMENTED_DIR=${JSON.stringify(instrumented)}`,
      `validate_frontend_coverage_artifacts ${JSON.stringify(temp)}`,
    ]);
    reset();
    assert.strictEqual(validate().status, 0);
    const straightLineReport = JSON.parse(validCoverage);
    const straightLineEntry = Object.values(straightLineReport)[0];
    straightLineEntry.fnMap = {};
    straightLineEntry.f = {};
    straightLineEntry.branchMap = {};
    straightLineEntry.b = {};
    expected.forEach(name => fs.writeFileSync(path.join(temp, name), JSON.stringify(straightLineReport)));
    assert.strictEqual(validate().status, 0, 'valid straight-line Istanbul coverage was rejected');
    fs.rmSync(path.join(temp, 'frontend-coverage-g4.json'));
    assert.notStrictEqual(validate().status, 0);
    reset();
    fs.writeFileSync(path.join(temp, 'e2e-coverage.json'), '{}');
    assert.notStrictEqual(validate().status, 0);

    const adversarial = [
      ['zero statements', entry => { entry.statementMap = {}; entry.s = {}; }],
      ['outside path', entry => { entry.path = '/tmp/not-in-this-run.js'; }],
      ['mismatched statement ids', entry => { entry.s = { '1': 1 }; }],
      ['mismatched function ids', entry => { entry.f = { '1': 1 }; }],
      ['mismatched branch counters', entry => { entry.b = { '0': [1] }; }],
      ['mixed empty branch start', entry => { entry.branchMap['0'].locations[0].start = {}; }],
      ['mixed empty branch end', entry => { entry.branchMap['0'].locations[0].end = {}; }],
      ['negative counter', entry => { entry.s['0'] = -1; }],
      ['non-finite counter', entry => { entry.b['0'][0] = null; }],
    ];
    for (const [label, mutate] of adversarial) {
      reset();
      const report = JSON.parse(validCoverage);
      mutate(Object.values(report)[0]);
      fs.writeFileSync(path.join(temp, 'e2e-coverage.json'), JSON.stringify(report));
      assert.notStrictEqual(validate().status, 0, label);
    }
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('cleanup removes temporary state, instrumented frontend, and server process', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-test-'));
  const work = path.join(temp, 'work');
  const instrumented = path.join(temp, 'public-instrumented');
  fs.mkdirSync(work);
  fs.mkdirSync(instrumented);
  const readOnlyCache = path.join(work, 'go-mod-cache', 'example@v1');
  fs.mkdirSync(readOnlyCache, { recursive: true });
  fs.writeFileSync(path.join(readOnlyCache, 'module.go'), 'package example\n');
  fs.chmodSync(path.join(readOnlyCache, 'module.go'), 0o444);
  fs.chmodSync(readOnlyCache, 0o555);
  const command = [
    'set -eu',
    `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(repoRoot)}`,
    'export COMBINED_COVERAGE_REPO_ROOT',
    `. ${JSON.stringify(library)}`,
    `WORK_DIR=${JSON.stringify(work)}`,
    `INSTRUMENTED_DIR=${JSON.stringify(instrumented)}`,
    'INSTRUMENTED_CREATED=1',
    'sleep 30 &',
    'SERVER_PID=$!',
    'server_pid=$SERVER_PID',
    'cleanup',
    '[ ! -e "$WORK_DIR" ]',
    '[ ! -e "$INSTRUMENTED_DIR" ]',
    '! kill -0 "$server_pid" 2>/dev/null',
  ].join('\n');
  const result = spawnSync('sh', ['-c', command], { cwd: repoRoot, encoding: 'utf8' });
  if (fs.existsSync(readOnlyCache)) fs.chmodSync(readOnlyCache, 0o755);
  fs.rmSync(temp, { recursive: true, force: true });
  assert.strictEqual(result.status, 0, result.stderr);
});

test('signal cleanup terminates the active tracked child', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-signal-'));
  const childPidFile = path.join(temp, 'child.pid');
  const workerScript = path.join(temp, 'worker.sh');
  const runnerScript = path.join(temp, 'runner.sh');
  fs.writeFileSync(workerScript, [
    '#!/bin/sh',
    "trap 'exit 0' TERM",
    `sh -c 'trap "" TERM; echo $$ > "$1"; while :; do sleep 1; done' sh "$1" &`,
    'wait',
  ].join('\n'));
  try {
    const inner = [
      'set -eu',
      `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(repoRoot)}`,
      'export COMBINED_COVERAGE_REPO_ROOT',
        `. ${JSON.stringify(library)}`,
      `WORK_DIR=${JSON.stringify(path.join(temp, 'work'))}`,
      'mkdir -p "$WORK_DIR"',
      'trap cleanup EXIT',
      "trap 'exit 130' HUP INT TERM",
      `run_tracked sh ${JSON.stringify(workerScript)} ${JSON.stringify(childPidFile)}`,
    ].join('\n');
    fs.writeFileSync(runnerScript, inner);
    const outer = [
      'set -eu',
      `sh ${JSON.stringify(runnerScript)} &`,
      'runner=$!',
      `while [ ! -s ${JSON.stringify(childPidFile)} ]; do sleep 0.05; done`,
      'kill -TERM "$runner"',
      'if wait "$runner"; then exit 1; else status=$?; fi',
      '[ "$status" -eq 130 ]',
      `child=$(cat ${JSON.stringify(childPidFile)})`,
      'attempt=0',
      'while kill -0 "$child" 2>/dev/null && [ "$attempt" -lt 20 ]; do sleep 0.05; attempt=$((attempt + 1)); done',
      '! kill -0 "$child" 2>/dev/null',
    ].join('\n');
    const result = spawnSync('sh', ['-c', outer], { cwd: repoRoot, encoding: 'utf8', timeout: 5000 });
    assert.strictEqual(result.status, 0, result.stderr || result.stdout);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

for (const trackedFunction of ['run_tracked', 'run_tracked_in_dir']) {
  test(`${trackedFunction} reaps surviving process-group descendants after a successful leader exit`, () => {
    const temp = fs.mkdtempSync(path.join(os.tmpdir(), `corescope-coverage-${trackedFunction}-`));
    const childPidFile = path.join(temp, 'child.pid');
    const worker = path.join(temp, 'worker.sh');
    fs.writeFileSync(worker, [
      '#!/bin/sh',
      `sh -c 'trap "exit 0" TERM; echo $$ > "$1"; while :; do sleep 1; done' sh "$1" </dev/null >/dev/null 2>&1 &`,
      'exit 0',
    ].join('\n'), { mode: 0o755 });
    try {
      const invocation = trackedFunction === 'run_tracked'
        ? `run_tracked sh ${JSON.stringify(worker)} ${JSON.stringify(childPidFile)}`
        : `run_tracked_in_dir ${JSON.stringify(temp)} sh ${JSON.stringify(worker)} ${JSON.stringify(childPidFile)}`;
      const result = sourceScript([
        invocation,
        `child=$(cat ${JSON.stringify(childPidFile)})`,
        '! kill -0 "$child" 2>/dev/null',
        '[ -z "$ACTIVE_PID" ]',
      ]);
      assert.strictEqual(result.status, 0, result.stderr || result.stdout);
    } finally {
      if (fs.existsSync(childPidFile)) {
        try { process.kill(Number(fs.readFileSync(childPidFile, 'utf8')), 'SIGKILL'); } catch (_) {}
      }
      fs.rmSync(temp, { recursive: true, force: true });
    }
  });
}

test('signal cleanup stops the active Docker fallback container', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-docker-signal-'));
  const fakeBin = path.join(temp, 'bin');
  const runnerScript = path.join(temp, 'runner.sh');
  const cidFile = path.join(temp, 'work', 'go-container.cid');
  const startedMarker = path.join(temp, 'started');
  const stoppedMarker = path.join(temp, 'stopped');
  const registry = path.join(temp, 'registry');
  const daemonPidFile = path.join(temp, 'daemon.pid');
  fs.mkdirSync(fakeBin);
  const fakeDocker = path.join(fakeBin, 'docker');
  fs.writeFileSync(fakeDocker, [
    '#!/bin/sh',
    'case "$1" in',
    '  run)',
    '    printf started > "$FAKE_DOCKER_STARTED_MARKER"',
    `    setsid sh -c 'sleep 0.3; printf fake-container > "$1"; while :; do sleep 1; done' sh "$FAKE_DOCKER_REGISTRY" &`,
    '    printf "%s" "$!" > "$FAKE_DOCKER_DAEMON_PID_FILE"',
    "    trap 'exit 143' TERM INT",
    '    while :; do sleep 1; done',
    '    ;;',
    '  ps) : ;;',
    '  inspect)',
    '    target=',
    '    for arg in "$@"; do target=$arg; done',
    '    if [ -s "$FAKE_DOCKER_REGISTRY" ]; then printf "%s" "${target#corescope-coverage-}"; fi',
    '    ;;',
    '  rm)',
    '    target=',
    '    for arg in "$@"; do target=$arg; done',
    '    if [ -n "$target" ]; then',
    '      kill "$(cat "$FAKE_DOCKER_DAEMON_PID_FILE")" 2>/dev/null || true',
    '      rm -f "$FAKE_DOCKER_REGISTRY"',
    '      printf stopped > "$FAKE_DOCKER_STOPPED_MARKER"',
    '    fi',
    '    ;;',
    'esac',
  ].join('\n'), { mode: 0o755 });
  try {
    const inner = [
      'set -eu',
      `COMBINED_COVERAGE_REPO_ROOT=${JSON.stringify(repoRoot)}`,
      'export COMBINED_COVERAGE_REPO_ROOT',
        `. ${JSON.stringify(library)}`,
      `WORK_DIR=${JSON.stringify(path.join(temp, 'work'))}`,
      `FAKE_DOCKER_STARTED_MARKER=${JSON.stringify(startedMarker)}`,
      `FAKE_DOCKER_STOPPED_MARKER=${JSON.stringify(stoppedMarker)}`,
      `FAKE_DOCKER_REGISTRY=${JSON.stringify(registry)}`,
      `FAKE_DOCKER_DAEMON_PID_FILE=${JSON.stringify(daemonPidFile)}`,
      `PATH=${JSON.stringify(`${fakeBin}:/bin`)}`,
      'export FAKE_DOCKER_STARTED_MARKER FAKE_DOCKER_STOPPED_MARKER FAKE_DOCKER_REGISTRY FAKE_DOCKER_DAEMON_PID_FILE PATH',
      'mkdir -p "$WORK_DIR/go-build-cache" "$WORK_DIR/go-mod-cache"',
      'trap cleanup EXIT',
      "trap 'exit 130' HUP INT TERM",
      'run_go cmd/server version',
    ].join('\n');
    fs.writeFileSync(runnerScript, inner);
    const outer = [
      'set -eu',
      `sh ${JSON.stringify(runnerScript)} &`,
      'runner=$!',
      `while [ ! -s ${JSON.stringify(startedMarker)} ]; do sleep 0.05; done`,
      'kill -TERM "$runner"',
      'if wait "$runner"; then exit 1; else status=$?; fi',
      '[ "$status" -eq 130 ]',
      `[ -s ${JSON.stringify(stoppedMarker)} ]`,
    ].join('\n');
    const result = spawnSync('sh', ['-c', outer], { cwd: repoRoot, encoding: 'utf8', timeout: 5000 });
    assert.strictEqual(result.status, 0, result.stderr || result.stdout);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('Docker client failure reconciles a delayed owned container before clearing ownership', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-coverage-docker-failure-'));
  const fakeBin = path.join(temp, 'bin');
  const work = path.join(temp, 'work');
  const registry = path.join(temp, 'registry');
  const stopped = path.join(temp, 'stopped');
  fs.mkdirSync(fakeBin);
  fs.mkdirSync(work);
  fs.writeFileSync(path.join(fakeBin, 'docker'), [
    '#!/bin/sh',
    'case "$1" in',
    '  run)',
    '    cidfile=; label=; shift',
    '    while [ "$#" -gt 0 ]; do',
    '      case "$1" in --cidfile) cidfile=$2; shift 2;; --label) label=$2; shift 2;; *) shift;; esac',
    '    done',
    `    setsid sh -c 'sleep 1.5; printf owned-cid > "$1"; printf "%s" "$2" > "$3"' sh "$cidfile" "$label" "$FAKE_DOCKER_REGISTRY" >/dev/null 2>&1 &`,
    '    exit 42',
    '    ;;',
    '  ps)',
    '    [ ! -s "$FAKE_DOCKER_REGISTRY" ] || printf "owned-cid\\n"',
    '    exit 0',
    '    ;;',
    '  inspect)',
    '    [ -s "$FAKE_DOCKER_REGISTRY" ] && cut -d= -f2- "$FAKE_DOCKER_REGISTRY"',
    '    ;;',
    '  rm)',
    '    rm -f "$FAKE_DOCKER_REGISTRY"',
    '    printf stopped > "$FAKE_DOCKER_STOPPED"',
    '    ;;',
    'esac',
  ].join('\n'), { mode: 0o755 });
  try {
    const result = sourceScript([
      `WORK_DIR=${JSON.stringify(work)}`,
      'mkdir -p "$WORK_DIR/go-build-cache" "$WORK_DIR/go-mod-cache"',
      'if run_go cmd/server version; then exit 90; else status=$?; fi',
      '[ "$status" -eq 42 ]',
      '[ -s "$FAKE_DOCKER_STOPPED" ]',
      '[ -z "$ACTIVE_DOCKER_CIDFILE" ]',
      '[ -z "$ACTIVE_DOCKER_NAME" ]',
      '[ -z "$ACTIVE_DOCKER_LABEL" ]',
    ], {
      PATH: `${fakeBin}:/bin`,
      FAKE_DOCKER_REGISTRY: registry,
      FAKE_DOCKER_STOPPED: stopped,
    });
    assert.strictEqual(result.status, 0, result.stderr || result.stdout);
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

test('package coverage commands name the artifacts they actually produce', () => {
  const pkg = JSON.parse(fs.readFileSync(path.join(repoRoot, 'package.json'), 'utf8'));
  assert.strictEqual(pkg.scripts['test:coverage'], 'sh scripts/combined-coverage.sh --go-only');
  assert.strictEqual(pkg.scripts['test:full-coverage'], 'sh scripts/combined-coverage.sh');
  assert.match(pkg.scripts['test:manifest'], /combined-coverage\.test\.js/);
});

if (!process.exitCode) console.log(`combined coverage orchestration: ${passed} tests passed`);
