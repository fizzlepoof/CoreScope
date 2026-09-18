package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicJSONEndpointsRejectMalformedJSON(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "batch observations", path: "/api/packets/observations"},
		{name: "packet decode", path: "/api/decode"},
	}

	srv := &Server{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(`{"broken"`))
			rec := httptest.NewRecorder()

			switch tt.path {
			case "/api/packets/observations":
				srv.handleBatchObservations(rec, req)
			case "/api/decode":
				srv.handleDecode(rec, req)
			}

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestPublicJSONEndpointsRejectTrailingJSON(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{name: "batch observations", path: "/api/packets/observations", body: `{"hashes":[]} {"hashes":[]}`},
		{name: "packet decode", path: "/api/decode", body: `{"hex":"0200"} {"hex":"0200"}`},
	}

	srv := &Server{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			switch tt.path {
			case "/api/packets/observations":
				srv.handleBatchObservations(rec, req)
			case "/api/decode":
				srv.handleDecode(rec, req)
			}

			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
		})
	}
}

func TestPublicJSONEndpointsRejectOversizedBodies(t *testing.T) {
	tests := []struct {
		name string
		path string
		body string
	}{
		{
			name: "batch observations",
			path: "/api/packets/observations",
			body: `{"padding":"` + strings.Repeat("a", 64<<10) + `","hashes":[]}`,
		},
		{
			name: "packet decode",
			path: "/api/decode",
			body: `{"padding":"` + strings.Repeat("a", 64<<10) + `","hex":"0200"}`,
		},
	}

	srv := &Server{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			switch tt.path {
			case "/api/packets/observations":
				srv.handleBatchObservations(rec, req)
			case "/api/decode":
				srv.handleDecode(rec, req)
			}

			if rec.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusRequestEntityTooLarge, rec.Body.String())
			}
		})
	}
}
