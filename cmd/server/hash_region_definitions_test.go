package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestConfigHashRegionDefinitionsReturnsPublicMetadata(t *testing.T) {
	srv := newTestAdminServer(t)
	body := []byte(`{"hashRegionDefinitions":[{"name":"#us-tn","description":"Tennessee regional scope","geometry":{"type":"Polygon","coordinates":[[[-90,35],[-81,35],[-81,37],[-90,35]]]}}]}`)
	putReq := httptest.NewRequest(http.MethodPut, "/api/admin/hash-regions", bytes.NewReader(body))
	putRecorder := httptest.NewRecorder()
	srv.handleAdminPutHashRegions(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", putRecorder.Code, putRecorder.Body.String())
	}

	req := httptest.NewRequest(http.MethodGet, "/api/config/hash-region-definitions", nil)
	recorder := httptest.NewRecorder()
	srv.handleConfigHashRegionDefinitions(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var definitions []hashRegionDefinitionPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &definitions); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(definitions) != 1 || definitions[0].Name != "#us-tn" || definitions[0].Description != "Tennessee regional scope" {
		t.Fatalf("definitions = %#v, want public region metadata", definitions)
	}
}

func TestAdminHashRegionDefinitionsRejectInvalidHierarchyAndGeometry(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing parent",
			body: `{"hashRegionDefinitions":[{"name":"#us-tn-bna","parentName":"#us-tn"}]}`,
		},
		{
			name: "parent cycle",
			body: `{"hashRegionDefinitions":[{"name":"#a","parentName":"#b"},{"name":"#b","parentName":"#a"}]}`,
		},
		{
			name: "unsupported geometry",
			body: `{"hashRegionDefinitions":[{"name":"#a","geometry":{"type":"LineString","coordinates":[[0,0],[1,1]]}}]}`,
		},
		{
			name: "unclosed polygon",
			body: `{"hashRegionDefinitions":[{"name":"#a","geometry":{"type":"Polygon","coordinates":[[[0,0],[1,0],[1,1],[0,1]]]}}]}`,
		},
		{
			name: "coordinate outside longitude range",
			body: `{"hashRegionDefinitions":[{"name":"#a","geometry":{"type":"Polygon","coordinates":[[[181,0],[1,0],[1,1],[181,0]]]}}]}`,
		},
		{
			name: "ring endpoints differ in altitude",
			body: `{"hashRegionDefinitions":[{"name":"#a","geometry":{"type":"Polygon","coordinates":[[[0,0,1],[1,0,1],[1,1,1],[0,0,2]]]}}]}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := newTestAdminServer(t)
			req := httptest.NewRequest(http.MethodPut, "/api/admin/hash-regions", bytes.NewBufferString(test.body))
			recorder := httptest.NewRecorder()
			srv.handleAdminPutHashRegions(recorder, req)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestAdminHashRegionDefinitionsRoundTrip(t *testing.T) {
	srv := newTestAdminServer(t)
	body := []byte(`{
		"hashRegionDefinitions": [
			{
				"name": "us-tn",
				"description": "Tennessee regional scope",
				"geometry": {"type":"Polygon","coordinates":[[[-90,35],[-81,35],[-81,37],[-90,35]]]}
			},
			{
				"name": "#us-tn-bna",
				"parentName": "us-tn",
				"description": "Middle Tennessee and Nashville",
				"geometry": {"type":"MultiPolygon","coordinates":[[[[-88,35],[-85,35],[-85,37],[-88,35]]]]}
			}
		]
	}`)

	putReq := httptest.NewRequest(http.MethodPut, "/api/admin/hash-regions", bytes.NewReader(body))
	putRecorder := httptest.NewRecorder()
	srv.handleAdminPutHashRegions(putRecorder, putReq)
	if putRecorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200: %s", putRecorder.Code, putRecorder.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/admin/hash-regions", nil)
	getRecorder := httptest.NewRecorder()
	srv.handleAdminGetHashRegions(getRecorder, getReq)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", getRecorder.Code, getRecorder.Body.String())
	}

	var response struct {
		HashRegions []string `json:"hashRegions"`
		Definitions []struct {
			Name        string          `json:"name"`
			ParentName  string          `json:"parentName"`
			Description string          `json:"description"`
			Geometry    json.RawMessage `json:"geometry"`
		} `json:"hashRegionDefinitions"`
	}
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}
	if len(response.HashRegions) != 2 || response.HashRegions[0] != "#us-tn" || response.HashRegions[1] != "#us-tn-bna" {
		t.Fatalf("hashRegions = %v, want normalized names", response.HashRegions)
	}
	if len(response.Definitions) != 2 {
		t.Fatalf("hashRegionDefinitions = %#v, want 2 entries", response.Definitions)
	}
	if got := response.Definitions[1]; got.ParentName != "#us-tn" || got.Description != "Middle Tennessee and Nashville" {
		t.Fatalf("child definition = %#v, want normalized parent and description", got)
	}
	var geometry struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(response.Definitions[0].Geometry, &geometry); err != nil {
		t.Fatalf("decode geometry: %v", err)
	}
	if geometry.Type != "Polygon" {
		t.Fatalf("geometry type = %q, want Polygon", geometry.Type)
	}
}
