#!/usr/bin/env node
'use strict';

const assert = require('assert');
const fs = require('fs');
const os = require('os');
const path = require('path');
const { EventEmitter } = require('events');
const { createFailureDiagnostics } = require('../e2e-failure-diagnostics');

let passed = 0;
function test(name, fn) {
  Promise.resolve().then(fn).then(() => {
    passed++;
    console.log(`  PASS ${name}`);
  }).catch(error => {
    console.error(`  FAIL ${name}: ${error.stack || error.message}`);
    process.exitCode = 1;
  });
}

async function main() {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'corescope-e2e-diagnostics-'));
  try {
    const page = new EventEmitter();
    const context = new EventEmitter();
    const browser = new EventEmitter();
    const cdp = new EventEmitter();
    cdp.send = async () => {};
    const diagnostics = createFailureDiagnostics({
      page,
      context,
      browser,
      cdp,
      baseUrl: 'http://127.0.0.1:1',
      outputDir: temp,
      capacity: 5,
    });
    const request = {
      url: () => 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.js?access_token=request-secret#request-fragment',
      resourceType: () => 'script',
      failure: () => ({ errorText: 'net::ERR_TIMED_OUT' }),
    };
    const pendingRequest = {
      url: () => 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css?api_key=response-secret#response-fragment',
      resourceType: () => 'stylesheet',
    };
    const response = {
      url: pendingRequest.url,
      status: () => 200,
      request: () => pendingRequest,
    };
    page.emit('request', request);
    page.emit('requestfailed', request);
    page.emit('request', pendingRequest);
    page.emit('response', response);
    page.emit('pageerror', new Error('page exploded'));
    cdp.emit('Page.domContentEventFired', { timestamp: 42 });

    await diagnostics.capture({ test: 'navigation regression', error: new Error('goto timeout') });
    const evidence = JSON.parse(fs.readFileSync(path.join(temp, 'e2e-navigation-diagnostics.json'), 'utf8'));
    assert.strictEqual(evidence.test, 'navigation regression');
    assert.match(evidence.error, /goto timeout/);
    assert(evidence.events.some(event => event.kind === 'requestfailed' && event.resourceType === 'script'));
    assert(evidence.events.some(event => event.kind === 'domcontentloaded'));
    assert(evidence.events.some(event => event.kind === 'pageerror'));
    assert.deepStrictEqual(evidence.inFlightRequests, [
      {
        url: 'https://unpkg.com/leaflet@1.9.4/dist/leaflet.css',
        resourceType: 'stylesheet',
        startedAt: evidence.events.find(event => event.kind === 'request' && event.url.startsWith('https://unpkg.com/leaflet@1.9.4/dist/leaflet.css')).at,
      },
    ]);
    const persistedEvidence = fs.readFileSync(path.join(temp, 'e2e-navigation-diagnostics.json'), 'utf8');
    for (const secret of ['request-secret', 'request-fragment', 'response-secret', 'response-fragment']) {
      assert(!persistedEvidence.includes(secret), `persisted evidence must not contain ${secret}`);
    }
    assert(fs.existsSync(path.join(temp, 'e2e-http-probes.txt')));
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
  console.log(`e2e failure diagnostics: ${passed + 1} tests passed`);
}

main().catch(error => {
  console.error(error.stack || error.message);
  process.exitCode = 1;
});
