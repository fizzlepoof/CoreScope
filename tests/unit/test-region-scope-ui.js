#!/usr/bin/env node
'use strict';

const assert = require('assert');
const fs = require('fs');

const index = fs.readFileSync('public/index.html', 'utf8');
const app = fs.readFileSync('public/app.js', 'utf8');
const scopeJS = fs.readFileSync('public/region-scope.js', 'utf8');
const scopeCSS = fs.readFileSync('public/region-scope.css', 'utf8');
const bottomNav = fs.readFileSync('public/bottom-nav.js', 'utf8');
const testAll = fs.readFileSync('test-all.sh', 'utf8');
const packageJSON = JSON.parse(fs.readFileSync('package.json', 'utf8'));
const testManifest = JSON.parse(fs.readFileSync('tests/manifest.json', 'utf8'));
const adminHTML = fs.readFileSync('public/admin/hash-regions.html', 'utf8');
const adminJS = fs.readFileSync('public/admin/hash-regions.js', 'utf8');

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
assert.match(scopeJS, /<option value="">No choice<\/option>/, 'home/default selectors start with no choice');
assert.match(scopeJS, /copy-region-mutations/, 'hierarchy mutations have a separate copy control');
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
assert.match(scopeCSS, /var\(--region-scope-color\)/, 'region cards visibly use their assigned colors');
assert.match(testAll, /run-manifest\.js --profile local-package-and-test-all/, 'canonical full test runner delegates to the manifest orchestrator');
assert.ok(testManifest.tests.some(test => test.path === 'test-region-scope-e2e.js' && test.suite === 'e2e' && test.status === 'active'), 'canonical manifest includes real Chromium coverage');
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

console.log('tests/unit/test-region-scope-ui.js: all tests passed');
