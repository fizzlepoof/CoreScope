/* Public Region Scope Helper page. */
(function () {
  'use strict';

  var map = null;
  var marker = null;
  var boundaryLayer = null;
  var definitions = [];
  var selected = [];
  var recommendationDetails = [];
  var manualAdditions = new Set();
  var manualRemovals = new Set();
  var lastRecommendationPoint = null;
  var rowByName = new Map();
  var loadGeneration = 0;
  var loadController = null;
  var definitionsCache = null;

  function element(id) { return document.getElementById(id); }

  function color(token, fallback) {
    var value = getComputedStyle(document.documentElement).getPropertyValue(token).trim();
    return value || fallback;
  }

  function selectedAncestors(name) {
    var byName = new Map(definitions.map(function (definition) { return [definition.name, definition]; }));
    var names = [];
    var current = name;
    var seen = new Set();
    while (current && byName.has(current) && !seen.has(current)) {
      seen.add(current);
      names.push(current);
      current = byName.get(current).parentName || '';
    }
    return names;
  }

  function reconcileSelection() {
    selected = RegionScopeHelpers.reconcileRecommendationSelection(
      definitions,
      recommendationDetails.map(function (item) { return item.definition.name; }),
      Array.from(manualAdditions),
      Array.from(manualRemovals)
    );
  }

  function createRow(definition) {
    var label = document.createElement('label');
    label.className = 'region-scope-item';
    label.dataset.regionName = definition.name;
    var checkbox = document.createElement('input');
    checkbox.type = 'checkbox';
    checkbox.setAttribute('aria-label', 'Select ' + definition.name);
    var text = document.createElement('span');
    var name = document.createElement('span');
    name.className = 'region-scope-name';
    name.textContent = definition.name;
    var description = document.createElement('span');
    description.className = 'region-scope-description';
    description.textContent = definition.description || 'No description provided.';
    text.appendChild(name);
    text.appendChild(description);
    var badge = document.createElement('span');
    badge.className = 'region-scope-badge';
    checkbox.addEventListener('change', function () {
      if (checkbox.checked) {
        manualAdditions.add(definition.name);
        selectedAncestors(definition.name).forEach(function (ancestor) { manualRemovals.delete(ancestor); });
      } else {
        manualAdditions.delete(definition.name);
        manualRemovals.add(definition.name);
      }
      reconcileSelection();
      updateRowsAndCommands();
      element('region-scope-status').textContent =
        (checkbox.checked ? 'Added ' : 'Removed ') + definition.name + ' as a manual override.';
    });
    label.appendChild(checkbox);
    label.appendChild(text);
    label.appendChild(badge);
    rowByName.set(definition.name, { root: label, checkbox: checkbox, badge: badge });
    return label;
  }

  function setCommandStage(id, commands, emptyText) {
    var output = element(id);
    output.textContent = commands.length ? commands.join('\n') : emptyText;
    output.dataset.copyText = commands.join('\n');
  }

  function updateSelector(id) {
    var select = element(id);
    var previous = select.value;
    select.replaceChildren();
    var none = document.createElement('option');
    none.value = '';
    none.textContent = 'No choice';
    select.appendChild(none);
    selected.slice().sort().forEach(function (name) {
      var option = document.createElement('option');
      option.value = name;
      option.textContent = name;
      select.appendChild(option);
    });
    select.value = selected.indexOf(previous) >= 0 ? previous : '';
  }

  function updateRowsAndCommands() {
    var selectedSet = new Set(selected);
    var reasonByName = new Map(recommendationDetails.map(function (item) {
      return [item.definition.name, item.reason];
    }));
    rowByName.forEach(function (row, name) {
      var reason = reasonByName.get(name);
      row.checkbox.checked = selectedSet.has(name);
      row.root.classList.toggle('is-selected', selectedSet.has(name));
      row.root.classList.toggle('is-recommended', !!reason);
      row.badge.textContent = reason === 'direct'
        ? 'Recommended: direct boundary match'
        : reason === 'ancestor' ? 'Recommended: required ancestor' : '';
    });
    updateSelector('region-scope-home');
    updateSelector('region-scope-default');
    renderCommands();
  }

  function renderCommands() {
    var warning = element('region-scope-warning');
    try {
      var generated = RegionScopeHelpers.buildCommands(definitions, selected, {
        home: element('region-scope-home').value,
        defaultRegion: element('region-scope-default').value,
      });
      setCommandStage('region-scope-mutations', generated.mutationCommands, 'Select at least one region to generate hierarchy mutations.');
      setCommandStage('region-scope-verification', generated.verificationCommands, 'region');
      setCommandStage('region-scope-home-default-commands',
        generated.homeCommands.concat(generated.defaultCommands),
        'No optional home/default choice.');
      setCommandStage('region-scope-save-command', generated.persistenceCommands, 'region save');
      warning.textContent = generated.warning + ' ' + generated.defaultWarning;
    } catch (error) {
      setCommandStage('region-scope-mutations', [], 'Commands unavailable: ' + error.message);
      setCommandStage('region-scope-verification', ['region'], 'region');
      setCommandStage('region-scope-home-default-commands', [], 'No optional home/default choice.');
      setCommandStage('region-scope-save-command', ['region save'], 'region save');
      warning.textContent = 'Correct the selected region hierarchy before using commands.';
    }
  }

  function showBoundaries() {
    if (!boundaryLayer) return;
    boundaryLayer.clearLayers();
    var recommendedSet = new Set(recommendationDetails.map(function (item) { return item.definition.name; }));
    definitions.forEach(function (definition) {
      if (!definition.geometry) return;
      L.geoJSON(definition.geometry, {
        style: function () {
          return {
            color: recommendedSet.has(definition.name) ? color('--accent', 'currentColor') : color('--text-muted', 'currentColor'),
            weight: recommendedSet.has(definition.name) ? 3 : 1,
            fillOpacity: recommendedSet.has(definition.name) ? 0.16 : 0.04,
          };
        },
        interactive: false,
      }).addTo(boundaryLayer);
    });
  }

  function chooseLocation(latlng) {
    if (!map) return;
    var nextPoint = [Number(latlng.lng), Number(latlng.lat)];
    if (lastRecommendationPoint &&
        (lastRecommendationPoint[0] !== nextPoint[0] || lastRecommendationPoint[1] !== nextPoint[1])) {
      manualRemovals.clear();
    }
    lastRecommendationPoint = nextPoint;
    if (marker) marker.setLatLng(latlng); else marker = L.marker(latlng).addTo(map);
    element('region-scope-lat').value = Number(latlng.lat).toFixed(5);
    element('region-scope-lon').value = Number(latlng.lng).toFixed(5);
    recommendationDetails = RegionScopeHelpers.recommendRegionDetails(definitions, nextPoint);
    reconcileSelection();
    updateRowsAndCommands();
    showBoundaries();
    var directCount = recommendationDetails.filter(function (item) { return item.reason === 'direct'; }).length;
    var ancestorCount = recommendationDetails.length - directCount;
    var status = element('region-scope-status');
    status.textContent = recommendationDetails.length
      ? 'Recommended ' + directCount + ' direct boundary match' + (directCount === 1 ? '' : 'es') +
        ' and ' + ancestorCount + ' required ancestor' + (ancestorCount === 1 ? '' : 's') +
        ' for ' + Number(latlng.lat).toFixed(5) + ', ' + Number(latlng.lng).toFixed(5) + '.'
      : 'No saved boundary contains ' + Number(latlng.lat).toFixed(5) + ', ' + Number(latlng.lng).toFixed(5) +
        '. Prior automatic geography was cleared; manual overrides remain.';
  }

  function recommendFromInputs() {
    var lat = Number(element('region-scope-lat').value);
    var lon = Number(element('region-scope-lon').value);
    if (!Number.isFinite(lat) || lat < -90 || lat > 90 || !Number.isFinite(lon) || lon < -180 || lon > 180) {
      element('region-scope-status').textContent = 'Enter a latitude from -90 to 90 and longitude from -180 to 180.';
      return;
    }
    chooseLocation({ lat: lat, lng: lon });
    map.setView([lat, lon], Math.max(map.getZoom(), 7));
  }

  function initMap() {
    map = L.map('region-scope-map', { preferCanvas: true }).setView([35.85, -86.4], 7);
    if (typeof window._applyTilesToNodeMap === 'function') {
      window._applyTilesToNodeMap(map);
    } else {
      console.warn('[region-scope] _applyTilesToNodeMap unavailable — using OSM fallback');
      L.tileLayer('https://tile.openstreetmap.org/{z}/{x}/{y}.png', {
        attribution: '© OpenStreetMap contributors', maxZoom: 19,
      }).addTo(map);
    }
    boundaryLayer = L.layerGroup().addTo(map);
    map.on('click', function (event) { chooseLocation(event.latlng); });
    showBoundaries();
  }

  function renderLoadedDefinitions(body, generation) {
    if (generation !== loadGeneration) return;
    definitions = Array.isArray(body) ? body : [];
    definitions.sort(function (a, b) { return a.name.localeCompare(b.name); });
    var list = element('region-scope-list');
    list.replaceChildren();
    rowByName.clear();
    if (!definitions.length) {
      var empty = document.createElement('p');
      empty.className = 'region-scope-empty';
      empty.textContent = 'No region definitions have been configured.';
      list.appendChild(empty);
    } else {
      var fragment = document.createDocumentFragment();
      definitions.forEach(function (definition) { fragment.appendChild(createRow(definition)); });
      list.appendChild(fragment);
    }
    if (lastRecommendationPoint) {
      chooseLocation({ lat: lastRecommendationPoint[1], lng: lastRecommendationPoint[0] });
    } else {
      reconcileSelection();
      updateRowsAndCommands();
      showBoundaries();
      element('region-scope-status').textContent = 'Click the map or enter coordinates for recommendations.';
    }
  }

  function loadDefinitions(generation) {
    var status = element('region-scope-status');
    var list = element('region-scope-list');
    list.replaceChildren();
    if (definitionsCache) {
      renderLoadedDefinitions(definitionsCache, generation);
      return Promise.resolve();
    }
    status.textContent = 'Loading saved region definitions…';
    loadController = typeof AbortController === 'function' ? new AbortController() : null;
    var options = loadController ? { signal: loadController.signal } : {};
    return fetch('/api/config/hash-region-definitions', options).then(function (response) {
      if (!response.ok) throw new Error('request failed (' + response.status + ')');
      return response.json();
    }).then(function (body) {
      if (generation !== loadGeneration) return;
      definitionsCache = Array.isArray(body) ? body : [];
      renderLoadedDefinitions(definitionsCache, generation);
    }).catch(function (error) {
      if (generation !== loadGeneration || (error && error.name === 'AbortError')) return;
      status.textContent = 'Could not load region definitions: ' + error.message;
    });
  }

  function copyStage(buttonId, outputId, label) {
    element(buttonId).addEventListener('click', function () {
      var text = element(outputId).dataset.copyText || '';
      if (!text) {
        element('region-scope-status').textContent = 'Nothing to copy for ' + label + '.';
        return;
      }
      navigator.clipboard.writeText(text).then(function () {
        element('region-scope-status').textContent = label + ' copied to clipboard.';
      }).catch(function () {
        element('region-scope-status').textContent = 'Copy failed. Select the stage and copy it manually.';
      });
    });
  }

  registerPage('region-scope', {
    init: function (container) {
      var generation = ++loadGeneration;
      definitions = []; selected = []; recommendationDetails = [];
      manualAdditions = new Set(); manualRemovals = new Set(); rowByName = new Map();
      lastRecommendationPoint = null;
      container.innerHTML =
        '<section class="region-scope-page" aria-labelledby="region-scope-title">' +
          '<header class="region-scope-header"><h2 id="region-scope-title">Region Scope Helper</h2>' +
          '<p>Choose a proposed repeater location, review the recommendation reasons, then inspect each command stage before applying it.</p></header>' +
          '<div class="region-scope-layout">' +
            '<section class="region-scope-card" aria-labelledby="region-map-title"><h3 id="region-map-title">Proposed location</h3>' +
              '<form id="region-scope-coordinate-form" class="region-scope-coordinate-form">' +
                '<label for="region-scope-lat">Latitude</label><input id="region-scope-lat" name="latitude" type="number" min="-90" max="90" step="any" required>' +
                '<label for="region-scope-lon">Longitude</label><input id="region-scope-lon" name="longitude" type="number" min="-180" max="180" step="any" required>' +
                '<button type="submit" class="btn-secondary" id="region-scope-recommend">Recommend regions</button>' +
              '</form>' +
              '<div id="region-scope-map" role="application" aria-label="Map for choosing a proposed repeater location"></div>' +
              '<p id="region-scope-status" class="region-scope-status" role="status" aria-live="polite"></p></section>' +
            '<section class="region-scope-card" aria-labelledby="region-list-title"><h3 id="region-list-title">Available regions</h3>' +
              '<p class="region-scope-status">Recommendation labels identify direct boundary matches and ancestors required for a valid hierarchy.</p>' +
              '<div id="region-scope-list" class="region-scope-list"></div>' +
              '<div class="region-scope-selectors">' +
                '<label for="region-scope-home">Optional home region</label><select id="region-scope-home"><option value="">No choice</option></select>' +
                '<label for="region-scope-default">Optional default scope</label><select id="region-scope-default"><option value="">No choice</option></select>' +
              '</div>' +
              '<p class="region-scope-warning">The <code>region default</code> command persists immediately. Inspect the hierarchy before applying it.</p>' +
              '<section class="region-scope-stage" aria-labelledby="region-mutations-title"><div class="region-scope-command-head"><h3 id="region-mutations-title">1. Hierarchy mutations</h3><button type="button" class="btn-secondary" id="copy-region-mutations">Copy mutations</button></div><pre id="region-scope-mutations" class="region-scope-commands" tabindex="0"></pre></section>' +
              '<section class="region-scope-stage" aria-labelledby="region-verification-title"><div class="region-scope-command-head"><h3 id="region-verification-title">2. Inspect result</h3><button type="button" class="btn-secondary" id="copy-region-verification">Copy verification</button></div><pre id="region-scope-verification" class="region-scope-commands" tabindex="0"></pre></section>' +
              '<section class="region-scope-stage" aria-labelledby="region-optional-title"><div class="region-scope-command-head"><h3 id="region-optional-title">3. Optional home/default</h3><button type="button" class="btn-secondary" id="copy-region-home-default">Copy optional choices</button></div><pre id="region-scope-home-default-commands" class="region-scope-commands" tabindex="0"></pre></section>' +
              '<section class="region-scope-stage" aria-labelledby="region-save-title"><div class="region-scope-command-head"><h3 id="region-save-title">4. Persist hierarchy</h3><button type="button" class="btn-secondary" id="copy-region-save">Copy save</button></div><pre id="region-scope-save-command" class="region-scope-commands" tabindex="0"></pre></section>' +
              '<p id="region-scope-warning" class="region-scope-warning"></p>' +
            '</section>' +
          '</div>' +
        '</section>';
      element('region-scope-coordinate-form').addEventListener('submit', function (event) {
        event.preventDefault();
        recommendFromInputs();
      });
      element('region-scope-home').addEventListener('change', renderCommands);
      element('region-scope-default').addEventListener('change', renderCommands);
      copyStage('copy-region-mutations', 'region-scope-mutations', 'Hierarchy mutations');
      copyStage('copy-region-verification', 'region-scope-verification', 'Verification command');
      copyStage('copy-region-home-default', 'region-scope-home-default-commands', 'Optional home/default commands');
      copyStage('copy-region-save', 'region-scope-save-command', 'Persistence command');
      initMap();
      loadDefinitions(generation);
    },
    destroy: function () {
      loadGeneration++;
      if (loadController) { loadController.abort(); loadController = null; }
      if (map) { map.remove(); map = null; }
      marker = null; boundaryLayer = null; rowByName.clear();
    },
  });
})();
