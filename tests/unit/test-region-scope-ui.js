#!/usr/bin/env node
'use strict';

const assert = require('assert');
const fs = require('fs');
const vm = require('vm');
const { fromRepositoryRoot } = require('../helpers/repository-root');

const index = fs.readFileSync(fromRepositoryRoot('public/index.html'), 'utf8');
const app = fs.readFileSync(fromRepositoryRoot('public/app.js'), 'utf8');
const scopeJS = fs.readFileSync(fromRepositoryRoot('public/region-scope.js'), 'utf8');
const scopeCoverageJS = fs.readFileSync(fromRepositoryRoot('public/scope-coverage.js'), 'utf8');
const liveJS = fs.readFileSync(fromRepositoryRoot('public/live.js'), 'utf8');
const regionsJS = fs.readFileSync(fromRepositoryRoot('public/regions.js'), 'utf8');
const scopeCSS = fs.readFileSync(fromRepositoryRoot('public/region-scope.css'), 'utf8');
const bottomNav = fs.readFileSync(fromRepositoryRoot('public/bottom-nav.js'), 'utf8');
const testAll = fs.readFileSync(fromRepositoryRoot('test-all.sh'), 'utf8');
const packageJSON = JSON.parse(fs.readFileSync(fromRepositoryRoot('package.json'), 'utf8'));
const testManifest = JSON.parse(fs.readFileSync(fromRepositoryRoot('tests/manifest.json'), 'utf8'));
const adminHTML = fs.readFileSync(fromRepositoryRoot('public/admin/hash-regions.html'), 'utf8');
const adminJS = fs.readFileSync(fromRepositoryRoot('public/admin/hash-regions.js'), 'utf8');

assert.match(index, /href="#\/tools\/region-scope"/, 'public navigation links the helper');
assert.match(index, /region-scope-helpers\.js\?v=__BUST__/, 'shared helper is loaded by browser');
assert.match(index, /region-scope\.js\?v=__BUST__/, 'helper page module is loaded');
assert.match(index, /region-scope\.css\?v=__BUST__/, 'helper page CSS is loaded');
assert.match(app, /region-scope/, 'router recognizes helper as a Tools route');
assert.match(scopeJS, /id="region-scope-lat"/, 'helper provides an accessible latitude input');
assert.match(scopeJS, /id="region-scope-lon"/, 'helper provides an accessible longitude input');
assert.match(scopeJS, /id="region-scope-recommend"/, 'helper provides a keyboard-operable recommendation button');
assert.match(scopeJS, /id="region-scope-home"/, 'helper provides an explicit home selector');
assert.match(scopeJS, /id="region-scope-default"/, 'helper provides an explicit default selector');
assert.match(scopeJS, /Home region marks this repeater’s local place in the displayed hierarchy/, 'home region has a concise operational description');
assert.match(scopeJS, /Default scope is attached to this repeater’s flooded adverts/, 'default scope has a concise operational description');
assert.match(scopeJS, /Creates or reparents the complete selected tree in one current-firmware command/, 'region def stage explains its effect');
assert.match(scopeJS, /If firmware reports an error, earlier mutations from that command may remain in memory/, 'region def help explains partial failure behavior');
assert.match(scopeJS, /Displays the repeater’s resulting in-memory tree so you can verify parentage before saving/, 'verification stage explains its safety purpose');
assert.match(scopeJS, /Writes the verified in-memory hierarchy to persistent storage/, 'save stage explains persistence');
assert.match(scopeJS, /<option value="">No choice<\/option>/, 'home/default selectors start with no choice');
assert.match(scopeJS, /<h3 id="region-mutations-title">1\. Define hierarchy \(one-shot\)<\/h3>/, 'helper labels the current one-shot region definition workflow');
assert.match(scopeJS, />Copy region def<\/button>/, 'hierarchy copy control names the current CLI command');
assert.match(scopeJS, /copy-region-mutations/, 'hierarchy definition has a separate copy control');
assert.match(scopeJS, /copy-region-verification/, 'verification has a separate copy control');
assert.match(scopeJS, /copy-region-home-default/, 'optional home/default has a separate copy control');
assert.match(scopeJS, /copy-region-save/, 'persistence has a separate copy control');
assert.match(scopeJS, /persists immediately/, 'UI explains immediate default persistence');
assert.match(scopeJS, /recommendRegionDetails/, 'UI exposes direct-vs-ancestor recommendation provenance');
assert.match(scopeJS, /Nearby border: about/, 'UI identifies nearby border suggestions and their distance');
assert.match(scopeJS, /item\.reason !== 'nearby'/, 'nearby border suggestions are not selected automatically');
assert.match(scopeJS, /id="region-scope-list-cue"/, 'available regions includes a visible scroll cue');
assert.match(scopeJS, /More regions below/, 'scroll cue explicitly tells operators when more regions are below');
assert.match(scopeJS, /regionColorToken/, 'regions receive deterministic distinct colors');
assert.match(scopeJS, /--region-scope-color/, 'region colors are exposed to list styling');
assert.match(scopeJS, /getComputedStyle\(probe\)\.color/, 'theme color tokens are resolved before Canvas map rendering');
assert.match(scopeJS, /addEventListener\('theme-changed', themeColorHandler\)/, 'map colors redraw after theme changes');
assert.match(scopeJS, /removeEventListener\('theme-changed', themeColorHandler\)/, 'theme color listener is removed on route teardown');
assert.match(scopeJS, /AbortController|loadGeneration/, 'definition loading guards against stale SPA fetches');
assert.match(scopeJS, /replaceChildren\(\)|textContent\s*=\s*''/, 'definition target is cleared before append');
assert.match(scopeJS, /_applyTilesToNodeMap/, 'helper uses the established tile provider path');
assert.doesNotMatch(scopeJS, /basemaps\.cartocdn\.com/, 'helper does not bypass configured tile providers');
assert.match(bottomNav, /region-scope/, 'mobile navigation exposes the helper route directly');
assert.match(scopeCSS, /@media \(max-width: 800px\)/, 'helper has mobile layout coverage');
assert.match(scopeCSS, /scrollbar-gutter:\s*stable/, 'available region list reserves visible scrollbar space');
assert.match(scopeCSS, /region-scope-list-frame\.is-scrollable/, 'scrollable list has a distinct visual treatment');
assert.match(scopeCSS, /var\(--region-scope-color,\s*var\(--accent\)\)/, 'region cards visibly use assigned colors with a safe pre-initialization fallback');
assert.match(testAll, /run-manifest\.js --profile local-package-and-test-all/, 'canonical full test runner delegates to the manifest orchestrator');
assert.ok(testManifest.tests.some(test => test.path === 'tests/e2e/test-region-scope-e2e.js' && test.suite === 'e2e' && test.status === 'active'), 'canonical manifest includes real Chromium coverage');
assert.doesNotMatch(packageJSON.scripts['test:unit'], /region-scope-e2e/, 'fast unit runner does not require a browser');

assert.match(adminHTML, /id="region-editor-list"/, 'admin has structured editor list');
assert.match(adminHTML, /id="county-select"/, 'admin has bundled county selection');
assert.match(adminHTML, /id="state-select"[\s\S]*multiple/, 'admin can select counties across multiple states');
assert.match(adminHTML, /id="geometry-map"/, 'admin has boundary preview/editor map');
assert.match(adminHTML, /id="geometry-coordinates"/, 'admin has accessible coordinate editing');
assert.match(adminHTML, /id="geojson-import"/, 'admin has GeoJSON import');
assert.match(adminJS, /hashRegionDefinitions/, 'admin persists structured definitions');
assert.match(adminJS, /\/geo\/us-counties\.geojson/, 'admin loads bundled nationwide counties once');
assert.match(adminJS, /orderDefinitionsParentFirst/, 'admin presents definitions in parent/child order');
assert.match(adminJS, /corescope-hash-regions-version/, 'saving definitions invalidates the public helper cache');
assert.doesNotMatch(adminJS, /\.innerHTML\s*=\s*[^'"`]/, 'admin does not inject untrusted values through innerHTML');

async function verifyCoverageColors() {
  const hashColor = {
    hashToHsl: (hex, theme) => `fallback:${hex}:${theme}`,
    hashToOutline: (hex, theme) => `outline:${hex}:${theme}`,
  };
  const context = {
    window: { HashColor: hashColor, matchMedia: () => ({ matches: false }) },
    HashColor: hashColor,
    document: {
      documentElement: { getAttribute: () => 'light' },
      getElementById: () => null,
    },
    localStorage: { getItem: () => null, setItem: () => {} },
    console,
  };
  vm.createContext(context);
  vm.runInContext(scopeCoverageJS, context);

  context.scopeCoverageSetRegionColors([
    { name: '#us-tn', color: '#12abef' },
    { name: '#invalid', color: 'blue' },
  ]);
  assert.strictEqual(context.scopeCoverageRegionColor('#us-tn'), '#12abef', 'saved Regions-tool color overrides the hash fallback');
  assert.match(context.scopeCoverageRegionColor('#invalid'), /^fallback:/, 'invalid saved colors retain deterministic fallback');
  assert.match(context.scopeCoverageRegionColor('#automatic'), /^fallback:/, 'regions without an assigned color retain deterministic fallback');
  const authoritativeGeometry = {
    type: 'Polygon',
    coordinates: [[[-88, 35], [-87, 35], [-87, 36], [-88, 35]]],
  };
  const combined = context.scopeCoverageCombineRegions([
    { name: '#us-tn', nodeCount: 2, hull: [[35, -88], [40, -70], [36, -87]] },
    { name: '#relay-only', nodeCount: 1, hull: [[50, -60]] },
  ], [
    { name: '#us-tn', color: '#12abef', geometry: authoritativeGeometry },
    { name: '#configured-empty', color: '#345678', geometry: null },
  ]);
  assert.deepStrictEqual(JSON.parse(JSON.stringify(combined)), [
    { name: '#us-tn', nodeCount: 2, geometry: authoritativeGeometry },
    { name: '#configured-empty', nodeCount: 0, geometry: null },
    { name: '#relay-only', nodeCount: 1, geometry: null },
  ], 'admin definitions own polygon geometry while relay-only regions retain counts without inferred polygons');
  assert.deepStrictEqual(
    JSON.parse(JSON.stringify(context.scopeCoverageGeometryLatLngs(authoritativeGeometry))),
    [[[35, -88], [35, -87], [36, -87], [35, -88]]],
    'admin GeoJSON coordinates are converted to Leaflet coordinates without using relay hull points'
  );
  assert.match(scopeCoverageJS, /api\('\/config\/hash-region-definitions'/, 'coverage overlay loads Regions-tool color definitions');
  assert.match(scopeCoverageJS, /var fill = scopeCoverageRegionColor\(region\.name\)/, 'coverage polygons use the shared region color resolver');
  assert.match(scopeCoverageJS, /scopeCoverageGeometryLatLngs\(region\.geometry\)/, 'coverage polygons use saved admin geometry');
  assert.match(liveJS, /createScopeCoverageOverlay\(/, 'Live map uses the shared coverage overlay');
  assert.match(regionsJS, /createScopeCoverageOverlay\(/, 'Regions tab uses the shared coverage overlay');
  assert.match(regionsJS, /scopeCoverageRegionColor\(primaryRegion \|\| regions\[0\]\)/, 'Regions node markers use the selected scope color resolver shared with polygons and legend swatches');

  context.api = async path => {
    if (path === '/config/hash-region-definitions') throw new Error('definitions unavailable');
    return { regions: [{ name: '#fallback', nodeCount: 1, hull: [[1, 2]] }] };
  };
  const overlay = context.createScopeCoverageOverlay({ on() {}, off() {}, removeLayer() {} }, {
    checkboxId: 'missing-toggle', labelId: 'missing-label', storageKey: 'test-scope-coverage',
  });
  await overlay.load();
  assert.strictEqual(overlay.getRegions().length, 1, 'definition failure does not suppress scope coverage data');

  const inferredShapeCalls = [];
  const failedDefinitionToggle = { checked: true, addEventListener() {} };
  context.document.getElementById = id => id === 'failed-definition-toggle'
    ? failedDefinitionToggle
    : (id === 'failed-definition-label' ? { style: {} } : null);
  context.L = {
    polygon() { inferredShapeCalls.push('polygon'); return {}; },
    polyline() { inferredShapeCalls.push('polyline'); return {}; },
    circleMarker() { inferredShapeCalls.push('circleMarker'); return {}; },
    layerGroup() { return { addTo() {} }; },
  };
  context.api = async path => {
    if (path === '/config/hash-region-definitions') throw new Error('definitions unavailable');
    return { regions: [{ name: '#observed-only', nodeCount: 4, hull: [[35, -88], [36, -87], [35, -86]] }] };
  };
  const failedDefinitionOverlay = context.createScopeCoverageOverlay({
    on() {}, off() {}, removeLayer() {}, hasLayer() { return true; },
  }, {
    checkboxId: 'failed-definition-toggle', labelId: 'failed-definition-label', storageKey: 'failed-definition-coverage',
  });
  await failedDefinitionOverlay.load();
  assert.deepStrictEqual(inferredShapeCalls, [], 'definition failure cannot render an observed hull as a polygon, line, or marker');
  assert.deepStrictEqual(
    JSON.parse(JSON.stringify(failedDefinitionOverlay.getRegions())),
    [{ name: '#observed-only', nodeCount: 4, geometry: null }],
    'definition failure preserves observed region membership and counts without treating its hull as geometry'
  );

  let renderedPolygon = null;
  const toggle = { checked: true, addEventListener() {} };
  const label = { style: {} };
  context.document.getElementById = id => id === 'coverage-toggle' ? toggle : (id === 'coverage-label' ? label : null);
  context.L = {
    polygon(latlngs, style) {
      renderedPolygon = { latlngs, style };
      return { setStyle() {}, bringToFront() {} };
    },
    layerGroup() { return { addTo() {} }; },
  };
  context.api = async path => path === '/config/hash-region-definitions'
    ? [{ name: '#us-tn', color: '#12abef', geometry: authoritativeGeometry }]
    : { regions: [{ name: '#us-tn', nodeCount: 2, hull: [[35, -88], [40, -70], [36, -87]] }] };
  const renderedOverlay = context.createScopeCoverageOverlay({
    on() {}, off() {}, removeLayer() {}, hasLayer() { return true; },
  }, {
    checkboxId: 'coverage-toggle', labelId: 'coverage-label', storageKey: 'rendered-scope-coverage',
  });
  await renderedOverlay.load();
  assert.deepStrictEqual(JSON.parse(JSON.stringify(renderedPolygon.latlngs)),
    [[[35, -88], [35, -87], [36, -87], [35, -88]]],
    'Live and Regions shared overlay renders the saved polygon, not the relay hull');
  assert.strictEqual(renderedPolygon.style.fillColor, '#12abef', 'saved region color styles the authoritative polygon');
  assert.strictEqual(renderedOverlay.getRegions()[0].nodeCount, 2, 'out-of-bound relays remain included in the displayed count');
}

verifyCoverageColors().then(() => {
  console.log('tests/unit/test-region-scope-ui.js: all tests passed');
}).catch(error => {
  console.error(error);
  process.exitCode = 1;
});
