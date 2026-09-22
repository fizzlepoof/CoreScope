#!/usr/bin/env node
'use strict';

const assert = require('assert');
const fs = require('fs');
const core = require('./public/admin/hash-regions.js');

const hierarchy = [
  { name: '#root', parentName: '', description: 'Root', geometry: null },
  { name: '#child', parentName: '#root', description: 'Child', geometry: null },
  { name: '#grandchild', parentName: '#child', description: 'Grandchild', geometry: null },
];
core.renameDefinition(hierarchy, 0, '#renamed');
assert.equal(hierarchy[0].name, '#renamed', 'rename updates node');
assert.equal(hierarchy[1].parentName, '#renamed', 'rename transactionally updates direct children');
assert.equal(hierarchy[2].parentName, '#child', 'unrelated parent remains intact');
assert.throws(() => core.renameDefinition(hierarchy, 0, '#child'), /duplicate/i, 'rename rejects duplicate atomically');
assert.equal(hierarchy[0].name, '#renamed', 'rejected rename does not mutate node');
assert.deepEqual(core.parentCandidateNames(hierarchy, 0), [], 'parent choices exclude every descendant');

const cyclic = [
  { name: '#a', parentName: '#b' },
  { name: '#b', parentName: '#c' },
  { name: '#c', parentName: '#a' },
];
assert.throws(() => core.validateHierarchy(cyclic), /cycle/i, 'multi-node cycle is rejected before submit');
assert.equal(core.normalizeColor('#12ABef'), '#12abef', 'custom colors are canonicalized');
assert.equal(core.normalizeColor(''), '', 'blank color keeps automatic assignment');
assert.throws(() => core.normalizeColor('red'), /#RRGGBB/, 'named colors are rejected');
assert.throws(() => core.normalizeColor('#123456; background:red'), /#RRGGBB/, 'CSS injection is rejected');

const unordered = [
  { name: '#grandchild', parentName: '#child' },
  { name: '#other', parentName: '' },
  { name: '#child', parentName: '#root' },
  { name: '#root', parentName: '' },
];
assert.deepStrictEqual(core.orderDefinitionsParentFirst(unordered).map((item) => [item.definition.name, item.depth]), [
  ['#other', 0], ['#root', 0], ['#child', 1], ['#grandchild', 2],
], 'admin rows are ordered as a deterministic parent/child tree with depth metadata');

const countyData = JSON.parse(fs.readFileSync('./public/geo/us-counties.geojson', 'utf8'));
const countyStates = new Set(countyData.features.map((feature) => feature.properties.STUSPS));
['TN', 'KY', 'AL'].forEach((state) => assert.ok(countyStates.has(state), 'county picker includes ' + state));
assert.ok(countyData.features.every((feature) => feature.properties.GEOID && feature.properties.STUSPS),
  'US county choices have stable cross-state identifiers');
const adminHTML = fs.readFileSync('./public/admin/hash-regions.html', 'utf8');
const adminJS = fs.readFileSync('./public/admin/hash-regions.js', 'utf8');
assert.match(adminHTML, /id="state-select"[\s\S]*multiple/, 'admin exposes a multi-state county filter');
assert.match(adminJS, /fetch\('\/geo\/us-counties\.geojson'\)/, 'admin loads the nationwide county dataset');
assert.match(adminHTML, /id="export-regions-btn"/, 'admin exposes one-file region export');
assert.match(adminHTML, /type="file"[^>]*id="region-backup-file"/, 'admin exposes JSON backup file selection');
assert.match(adminHTML, /value="merge"[\s\S]*value="replace"/, 'admin makes merge and replace modes explicit');
assert.match(adminHTML, /id="region-replace-confirm"/, 'admin requires a separate destructive replacement acknowledgement');
assert.match(adminJS, /\/api\/admin\/hash-regions\/export/, 'admin downloads the authenticated server export');
assert.match(adminJS, /\/api\/admin\/hash-regions\/import\?mode=[\s\S]*dryRun=true/, 'admin validates imports server-side before applying them');
assert.match(adminJS, /expectedRevision=' \+ encodeURIComponent\(expectedRevision\)/, 'admin binds apply to the exact dry-run state revision');
assert.match(adminJS, /mode === 'replace' \? '&confirm=true'/, 'admin only sends destructive confirmation for replace mode');
assert.equal(core.backupImportSummary({ mode: 'merge', added: 2, updated: 3, preserved: 4, total: 9 }),
  'Valid merge backup: 2 added, 3 updated, 4 preserved; 9 regions after import.');
assert.equal(core.backupImportSummary({ mode: 'replace', added: 1, updated: 2, removed: 6, total: 3 }),
  'Valid replace backup: 1 added, 2 updated, 6 removed; 3 regions after import.');
assert.equal(core.isCurrentBackupPreview(4, 4, 'replace', 'replace', 'backup-b', 'backup-b'), true,
  'latest preview for the current file and mode may authorize import');
assert.equal(core.isCurrentBackupPreview(3, 4, 'replace', 'replace', 'backup-a', 'backup-b'), false,
  'a stale reordered preview cannot authorize a newly selected backup');
assert.equal(core.isCurrentBackupPreview(4, 4, 'merge', 'replace', 'backup-b', 'backup-b'), false,
  'a preview for another mode cannot authorize import');

const polygonWithHole = {
  type: 'Polygon',
  coordinates: [
    [[0, 0], [8, 0], [8, 8], [0, 8], [0, 0]],
    [[1, 1], [2, 1], [2, 2], [1, 2], [1, 1]],
  ],
};
const moved = core.replacePolygonOuterRing(polygonWithHole, [[0, 0], [9, 0], [9, 9], [0, 9]]);
assert.deepEqual(moved.coordinates[1], polygonWithHole.coordinates[1], 'outer vertex drag preserves all holes');
assert.notStrictEqual(moved.coordinates[1], polygonWithHole.coordinates[1], 'result is safely cloned');

const multi = { type: 'MultiPolygon', coordinates: [polygonWithHole.coordinates, [[[20, 20], [21, 20], [21, 21], [20, 20]]]] };
const raw = core.geometryToEditableGeoJSON(multi);
assert.deepEqual(core.parseEditableGeoJSON(raw), multi, 'full editable GeoJSON repairs/applies MultiPolygon without loss');
assert.deepEqual(core.parseEditableGeoJSON(core.geometryToEditableGeoJSON(polygonWithHole)), polygonWithHole, 'full editable GeoJSON preserves Polygon holes');

let sampled = [];
sampled = core.sampleFreehandPoint(sampled, [0, 0], 0.01);
sampled = core.sampleFreehandPoint(sampled, [0.001, 0.001], 0.01);
sampled = core.sampleFreehandPoint(sampled, [0.02, 0.02], 0.01);
assert.equal(sampled.length, 2, 'freehand continuously samples meaningful pointer movement');

const under = core.payloadByteStatus({ hashRegionDefinitions: hierarchy });
assert.equal(under.overLimit, false);
assert.match(under.message, /under 1 MiB/i);
const over = core.payloadByteStatus({ hashRegionDefinitions: [{ name: '#x', description: 'x'.repeat(1024 * 1024) }] });
assert.equal(over.overLimit, true);
assert.match(over.message, /over 1 MiB/i);

function deferred() {
  let resolve;
  const promise = new Promise((done) => { resolve = done; });
  return { promise, resolve };
}

(async function testReorderedBackupValidationPromises() {
  let generation = 0;
  let currentText = '';
  let currentMode = 'merge';
  let authorizedPreview = '';

  function startValidation(text, mode, response) {
    const requestGeneration = ++generation;
    currentText = text;
    currentMode = mode;
    return response.promise.then((preview) => {
      if (core.isCurrentBackupPreview(
        requestGeneration, generation, mode, currentMode, text, currentText
      )) authorizedPreview = preview;
    });
  }

  const backupA = deferred();
  const backupB = deferred();
  const pendingA = startValidation('backup-a', 'replace', backupA);
  const pendingB = startValidation('backup-b', 'replace', backupB);
  backupB.resolve('preview-b');
  await pendingB;
  assert.equal(authorizedPreview, 'preview-b', 'latest backup preview authorizes import');
  backupA.resolve('preview-a');
  await pendingA;
  assert.equal(authorizedPreview, 'preview-b', 'late preview for the old backup is ignored');

  console.log('test-hash-region-admin.js: all tests passed');
}()).catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
