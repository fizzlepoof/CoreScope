'use strict';

const fs = require('fs');
const http = require('http');
const https = require('https');
const path = require('path');

function probe(url) {
  return new Promise(resolve => {
    const request = (url.startsWith('https:') ? https : http).get(url, { timeout: 2000 }, response => {
      response.resume();
      resolve(`${url} status=${response.statusCode}`);
    });
    request.once('timeout', () => request.destroy(new Error('timeout')));
    request.once('error', error => resolve(`${url} error=${error.message}`));
  });
}

function sanitizeURL(value) {
  try {
    const parsed = new URL(value);
    return `${parsed.origin}${parsed.pathname}`;
  } catch {
    return String(value).split(/[?#]/, 1)[0];
  }
}

function createFailureDiagnostics({ page, context, browser, cdp, baseUrl, outputDir, capacity = 100 }) {
  const events = [];
  const inFlightRequests = new Map();
  const record = (kind, fields = {}) => {
    const event = { at: new Date().toISOString(), kind, ...fields };
    events.push(event);
    if (events.length > capacity) events.shift();
    return event;
  };
  const describeRequest = request => ({
    url: sanitizeURL(request.url()),
    resourceType: request.resourceType(),
  });

  page.on('request', request => {
    const description = describeRequest(request);
    const event = record('request', description);
    inFlightRequests.set(request, { ...description, startedAt: event.at });
  });
  page.on('response', response => record('response', {
    url: sanitizeURL(response.url()),
    status: response.status(),
    resourceType: response.request().resourceType(),
  }));
  page.on('requestfinished', request => {
    inFlightRequests.delete(request);
    record('requestfinished', describeRequest(request));
  });
  page.on('requestfailed', request => {
    inFlightRequests.delete(request);
    record('requestfailed', {
      ...describeRequest(request),
      error: request.failure()?.errorText || 'unknown',
    });
  });
  page.on('pageerror', error => record('pageerror', { error: error.message }));
  page.on('crash', () => record('crash'));
  page.on('close', () => record('close'));
  context.on('close', () => record('contextclose'));
  browser.on('disconnected', () => record('browserdisconnected'));
  if (cdp) cdp.on('Page.domContentEventFired', event => record('domcontentloaded', { timestamp: event.timestamp }));

  return {
    async capture({ test, error }) {
      if (!outputDir) return;
      fs.mkdirSync(outputDir, { recursive: true });
      fs.writeFileSync(path.join(outputDir, 'e2e-navigation-diagnostics.json'), JSON.stringify({
        capturedAt: new Date().toISOString(),
        baseUrl,
        test,
        error: error.message,
        events,
        inFlightRequests: [...inFlightRequests.values()],
      }, null, 2));
      const [root, health] = await Promise.all([
        probe(`${baseUrl}/`),
        probe(`${baseUrl}/api/healthz`),
      ]);
      fs.writeFileSync(path.join(outputDir, 'e2e-http-probes.txt'), `${root}\n${health}\n`);
    },
  };
}

module.exports = { createFailureDiagnostics };
