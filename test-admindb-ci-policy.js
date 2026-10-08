'use strict';

// This guard runs explicitly in the canonical go-test job. Server ./... does
// not cross the separate internal/admindb go.mod boundary.
const assert = require('assert');
const fs = require('fs');
const path = require('path');
const workflow = fs.readFileSync(path.join(__dirname, '.github/workflows/deploy.yml'), 'utf8');
const job = workflow.match(/^  go-test:\n([\s\S]*?)(?=^  [\w-]+:|$(?![\s\S]))/m);
assert.ok(job, 'canonical go-test job must exist');
const steps = job[1].split(/^      - name: /m).slice(1);
const adminStep = steps.find(step => /\n          cd internal\/admindb\n/.test(step));
assert.ok(adminStep, 'canonical go-test job must explicitly enter the separate admindb module');
assert.ok(!/^        (?:if|continue-on-error):/m.test(adminStep), 'admindb tests must not be conditional or allowed to fail');
assert.ok(/\n          set -e -o pipefail\n/.test(adminStep), 'admindb step must propagate failures');
assert.ok(/\n          go test -timeout 15m -race \.\/\.\.\.\n/.test(adminStep), 'admindb must execute all packages with race detection and a bounded timeout');
assert.ok(steps.some(step => /\n          node test-admindb-ci-policy\.js\n/.test(step)), 'canonical CI must execute this policy regression');
console.log('PASS: canonical CI executes admindb module tests and its policy regression');
