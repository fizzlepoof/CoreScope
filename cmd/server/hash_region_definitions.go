package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/meshcore-analyzer/admindb"
)

const maxHashRegionDescriptionLen = 2000

type hashRegionDefinitionPayload struct {
	Name        string          `json:"name"`
	ParentName  string          `json:"parentName,omitempty"`
	Description string          `json:"description,omitempty"`
	Geometry    json.RawMessage `json:"geometry,omitempty"`
}

type hashRegionDefinitionsResponse struct {
	HashRegions           []string                      `json:"hashRegions"`
	HashRegionDefinitions []hashRegionDefinitionPayload `json:"hashRegionDefinitions"`
}

func newHashRegionDefinitionsResponse(definitions []admindb.HashRegionDefinition) hashRegionDefinitionsResponse {
	names := make([]string, 0, len(definitions))
	for _, definition := range definitions {
		names = append(names, definition.Name)
	}
	return hashRegionDefinitionsResponse{
		HashRegions:           names,
		HashRegionDefinitions: hashRegionDefinitionPayloads(definitions),
	}
}

func cleanHashRegionDefinitions(input []hashRegionDefinitionPayload) ([]admindb.HashRegionDefinition, error) {
	if len(input) > maxHashRegionEntries {
		return nil, fmt.Errorf("too many hash regions (max %d)", maxHashRegionEntries)
	}

	definitions := make([]admindb.HashRegionDefinition, 0, len(input))
	byName := make(map[string]bool, len(input))
	for _, raw := range input {
		name := normalizeHashRegionName(raw.Name)
		if name == "" {
			return nil, errors.New("hash region name is required")
		}
		if utf8.RuneCountInString(name) > maxHashRegionNameLen {
			return nil, fmt.Errorf("hash region name %q exceeds %d characters", name, maxHashRegionNameLen)
		}
		if byName[name] {
			return nil, fmt.Errorf("duplicate hash region %q", name)
		}
		byName[name] = true

		description := strings.TrimSpace(raw.Description)
		if utf8.RuneCountInString(description) > maxHashRegionDescriptionLen {
			return nil, fmt.Errorf("hash region description for %q exceeds %d characters", name, maxHashRegionDescriptionLen)
		}
		geometryJSON, err := normalizeHashRegionGeometry(raw.Geometry)
		if err != nil {
			return nil, fmt.Errorf("invalid geometry for %q: %w", name, err)
		}
		definitions = append(definitions, admindb.HashRegionDefinition{
			Name:         name,
			ParentName:   normalizeHashRegionName(raw.ParentName),
			Description:  description,
			GeometryJSON: geometryJSON,
		})
	}

	parents := make(map[string]string, len(definitions))
	for _, definition := range definitions {
		if definition.ParentName != "" {
			if definition.ParentName == definition.Name {
				return nil, fmt.Errorf("hash region %q cannot be its own parent", definition.Name)
			}
			if !byName[definition.ParentName] {
				return nil, fmt.Errorf("parent %q for hash region %q does not exist", definition.ParentName, definition.Name)
			}
		}
		parents[definition.Name] = definition.ParentName
	}
	if cycleAt := hashRegionParentCycle(parents); cycleAt != "" {
		return nil, fmt.Errorf("hash region hierarchy contains a cycle at %q", cycleAt)
	}
	return definitions, nil
}

func hashRegionParentCycle(parents map[string]string) string {
	const (
		visiting = 1
		visited  = 2
	)
	state := make(map[string]int, len(parents))
	var visit func(string) string
	visit = func(name string) string {
		switch state[name] {
		case visiting:
			return name
		case visited:
			return ""
		}
		state[name] = visiting
		if parent := parents[name]; parent != "" {
			if cycleAt := visit(parent); cycleAt != "" {
				return cycleAt
			}
		}
		state[name] = visited
		return ""
	}
	for name := range parents {
		if cycleAt := visit(name); cycleAt != "" {
			return cycleAt
		}
	}
	return ""
}

func normalizeHashRegionGeometry(raw json.RawMessage) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return "", nil
	}
	var geometry struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}
	if err := json.Unmarshal(raw, &geometry); err != nil {
		return "", errors.New("must be valid GeoJSON")
	}
	if len(geometry.Coordinates) == 0 || bytes.Equal(bytes.TrimSpace(geometry.Coordinates), []byte("null")) {
		return "", errors.New("coordinates are required")
	}
	switch geometry.Type {
	case "Polygon":
		var polygon [][][]float64
		if err := json.Unmarshal(geometry.Coordinates, &polygon); err != nil {
			return "", errors.New("Polygon coordinates are invalid")
		}
		if err := validateGeoJSONPolygon(polygon); err != nil {
			return "", err
		}
	case "MultiPolygon":
		var multiPolygon [][][][]float64
		if err := json.Unmarshal(geometry.Coordinates, &multiPolygon); err != nil || len(multiPolygon) == 0 {
			return "", errors.New("MultiPolygon coordinates are invalid")
		}
		for _, polygon := range multiPolygon {
			if err := validateGeoJSONPolygon(polygon); err != nil {
				return "", err
			}
		}
	default:
		return "", errors.New("type must be Polygon or MultiPolygon")
	}

	canonical, err := json.Marshal(struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	}{Type: geometry.Type, Coordinates: geometry.Coordinates})
	if err != nil {
		return "", fmt.Errorf("encode geometry: %w", err)
	}
	return string(canonical), nil
}

func validateGeoJSONPolygon(polygon [][][]float64) error {
	if len(polygon) == 0 {
		return errors.New("Polygon must contain at least one ring")
	}
	for _, ring := range polygon {
		if len(ring) < 4 {
			return errors.New("each Polygon ring must contain at least four positions")
		}
		for _, position := range ring {
			if len(position) < 2 || position[0] < -180 || position[0] > 180 || position[1] < -90 || position[1] > 90 {
				return errors.New("positions must contain longitude [-180,180] and latitude [-90,90]")
			}
		}
		first, last := ring[0], ring[len(ring)-1]
		if len(first) != len(last) {
			return errors.New("Polygon rings must be closed")
		}
		for i := range first {
			if first[i] != last[i] {
				return errors.New("Polygon rings must be closed")
			}
		}
	}
	return nil
}

func hashRegionDefinitionPayloads(definitions []admindb.HashRegionDefinition) []hashRegionDefinitionPayload {
	out := make([]hashRegionDefinitionPayload, 0, len(definitions))
	for _, definition := range definitions {
		var geometry json.RawMessage
		if definition.GeometryJSON != "" {
			geometry = json.RawMessage(definition.GeometryJSON)
		}
		out = append(out, hashRegionDefinitionPayload{
			Name:        definition.Name,
			ParentName:  definition.ParentName,
			Description: definition.Description,
			Geometry:    geometry,
		})
	}
	return out
}
