#!/usr/bin/env node
'use strict';

const assert = require('assert');
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

console.log('test-hash-region-admin.js: all tests passed');
