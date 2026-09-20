#!/usr/bin/env node
'use strict';
const { fromRepositoryRoot } = require('../helpers/repository-root');

const assert = require('assert');
const fs = require('fs');
const core = require(fromRepositoryRoot('public', 'admin', 'hash-regions.js'));

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

const countyData = JSON.parse(fs.readFileSync(
  fromRepositoryRoot('public', 'geo', 'us-counties.geojson'),
  'utf8'
));
const countyStates = new Set(countyData.features.map((feature) => feature.properties.STUSPS));
['TN', 'KY', 'AL'].forEach((state) => assert.ok(countyStates.has(state), 'county picker includes ' + state));
assert.ok(countyData.features.every((feature) => feature.properties.GEOID && feature.properties.STUSPS),
  'US county choices have stable cross-state identifiers');
const adminHTML = fs.readFileSync(fromRepositoryRoot('public', 'admin', 'hash-regions.html'), 'utf8');
const adminJS = fs.readFileSync(fromRepositoryRoot('public', 'admin', 'hash-regions.js'), 'utf8');
assert.match(adminHTML, /id="state-select"[\s\S]*multiple/, 'admin exposes a multi-state county filter');
assert.match(adminJS, /fetch\('\/geo\/us-counties\.geojson'\)/, 'admin loads the nationwide county dataset');

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
