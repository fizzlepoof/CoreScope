// Tests for issue #1677: release fast-path workflow.
//
// These tests gate the workflow config (not Go code) by parsing the YAML
// files as text and asserting structural invariants. They follow the same
// "config gate" pattern as openapi_completeness_test.go.
//
//  1. .github/workflows/release-fast-path.yml MUST exist and own the
//     push.tags trigger for v-tags, with the two execution branches
//     (re-tag-via-crane on SHA match, fallback to deploy.yml otherwise).
//  2. .github/workflows/deploy.yml MUST NOT trigger on push.tags any
//     more — the fast-path workflow owns tag pushes to avoid double-fire.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const (
	fastPathWorkflowRel = "../../.github/workflows/release-fast-path.yml"
	deployWorkflowRel   = "../../.github/workflows/deploy.yml"
	releaseGuideRel     = "../../docs/agents/skills/corescope-release.md"
)

func TestReleaseFastPathWorkflowExists(t *testing.T) {
	abs, _ := filepath.Abs(fastPathWorkflowRel)
	raw, err := os.ReadFile(fastPathWorkflowRel)
	if err != nil {
		t.Fatalf("issue #1677: release-fast-path.yml missing at %s: %v", abs, err)
	}
	src := string(raw)

	// Trigger: push.tags matching semver v-tags.
	triggerRe := regexp.MustCompile(`(?m)^\s*tags:\s*\[\s*['"]v\[0-9\]\+\.\[0-9\]\+\.\[0-9\]\+['"]\s*\]`)
	if !triggerRe.MatchString(src) {
		t.Errorf("release-fast-path.yml: missing required push.tags trigger 'v[0-9]+.[0-9]+.[0-9]+'")
	}

	// The fast path only reads source and writes package tags. The fallback is
	// a local reusable-workflow call, so it must not need actions:write.
	for _, perm := range []string{"packages: write", "contents: read"} {
		if !strings.Contains(src, perm) {
			t.Errorf("release-fast-path.yml: missing required permission %q", perm)
		}
	}

	// Required markers covering both execution branches:
	//   - re-tag path: install crane, read :edge revision label, apply new tags
	//   - fallback path: synchronously call the existing deploy.yml pipeline
	required := []string{
		"imjasonh/setup-crane",                 // crane install action
		"org.opencontainers.image.revision",    // label inspected on :edge
		"IMAGE_OWNER",                          // fork-portable image owner
		":edge",                                // source tag we copy from
		"crane tag",                            // metadata-only retag
		"uses: ./.github/workflows/deploy.yml", // synchronous fallback
	}
	for _, need := range required {
		if !strings.Contains(src, need) {
			t.Errorf("release-fast-path.yml: missing required marker %q (issue #1677 fix-path)", need)
		}
	}
}

func TestDeployWorkflowNoLongerTriggersOnTags(t *testing.T) {
	raw, err := os.ReadFile(deployWorkflowRel)
	if err != nil {
		t.Fatalf("deploy.yml: %v", err)
	}
	// Extract the top-level `on:` block: from `^on:` up to the next
	// top-level YAML key (line that starts in column 0 with a letter).
	blockRe := regexp.MustCompile(`(?ms)^on:\s*\n(.*?)\n([a-zA-Z][a-zA-Z0-9_-]*:)`)
	m := blockRe.FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("deploy.yml: could not locate top-level on: block")
	}
	onBlock := m[1]
	if regexp.MustCompile(`(?m)^\s*tags:\s*\[`).MatchString(onBlock) {
		t.Errorf("deploy.yml: on: block still triggers on push.tags; the fast-path workflow (release-fast-path.yml) must own tag pushes to avoid double-fire (issue #1677).\non-block was:\n%s", onBlock)
	}
}

func TestReleaseFastPathResolvesAndValidatesEdgeDigest(t *testing.T) {
	raw, err := os.ReadFile(fastPathWorkflowRel)
	if err != nil {
		t.Fatalf("read release fast path: %v", err)
	}

	route := workflowStepBlock(string(raw), "route")
	if route == "" {
		t.Fatal("release-fast-path.yml: route step is missing")
	}
	for _, required := range []string{
		`EDGE_DIGEST="$(crane digest "$EDGE_REF")"`,
		`[[ ! "$EDGE_DIGEST" =~ ^sha256:[0-9a-f]{64}$ ]]`,
		`EDGE_REF_WITH_DIGEST="${EDGE_REF%:edge}@${EDGE_DIGEST}"`,
		`crane config "$EDGE_REF_WITH_DIGEST"`,
		`printf 'edge_digest=%s\n' "$EDGE_DIGEST" >> "$GITHUB_OUTPUT"`,
	} {
		if !strings.Contains(route, required) {
			t.Errorf("release-fast-path.yml: route step must resolve, validate, inspect, and safely output one immutable edge digest; missing %q", required)
		}
	}
	if strings.Contains(route, `crane config "$EDGE_REF"`) {
		t.Error("release-fast-path.yml: route step must inspect the immutable digest reference, not mutable :edge")
	}
}

func TestReleaseFastPathRetagsOnlyResolvedDigest(t *testing.T) {
	raw, err := os.ReadFile(fastPathWorkflowRel)
	if err != nil {
		t.Fatalf("read release fast path: %v", err)
	}

	retag := workflowNamedStepBlock(string(raw), "Re-tag :edge as release (fast path)")
	if retag == "" {
		t.Fatal("release-fast-path.yml: fast-path retag step is missing")
	}
	for _, required := range []string{
		`EDGE_DIGEST: ${{ steps.route.outputs.edge_digest }}`,
		`SRC="ghcr.io/${OWNER}/corescope@${EDGE_DIGEST}"`,
		`for NEW_TAG in "$RELEASE_TAG" "$RELEASE_MAJOR_MINOR" "$RELEASE_MAJOR" latest; do`,
		`crane tag "$SRC" "$NEW_TAG"`,
	} {
		if !strings.Contains(retag, required) {
			t.Errorf("release-fast-path.yml: retag step must use the route step's immutable digest for every release tag; missing %q", required)
		}
	}

	runBlocks := workflowRunBlocks(retag)
	if len(runBlocks) != 1 {
		t.Fatalf("release-fast-path.yml: expected one retag run block, got %d", len(runBlocks))
	}
	if strings.Contains(runBlocks[0], ":edge") {
		t.Error("release-fast-path.yml: retag commands must never source mutable :edge")
	}
	craneTag := regexp.MustCompile(`(?m)^\s*crane tag\s+(\S+)\s+`)
	matches := craneTag.FindAllStringSubmatch(runBlocks[0], -1)
	if len(matches) == 0 {
		t.Fatal("release-fast-path.yml: retag run block has no crane tag command")
	}
	for _, match := range matches {
		if match[1] != `"$SRC"` {
			t.Errorf("release-fast-path.yml: every crane tag source must be the immutable $SRC, got %s", match[1])
		}
	}
}

func TestReleaseShellsDoNotInterpolateExpressions(t *testing.T) {
	for _, workflow := range []string{fastPathWorkflowRel, deployWorkflowRel} {
		raw, err := os.ReadFile(workflow)
		if err != nil {
			t.Fatalf("read %s: %v", workflow, err)
		}
		for _, block := range workflowRunBlocks(string(raw)) {
			if strings.Contains(block, "${{") {
				t.Errorf("%s: GitHub expressions must enter shell steps through env, not direct interpolation:\n%s", workflow, block)
			}
		}
	}
}

func TestReleaseFallbackDoesNotInheritCustomSecrets(t *testing.T) {
	raw, err := os.ReadFile(fastPathWorkflowRel)
	if err != nil {
		t.Fatalf("read release fast path: %v", err)
	}

	fallback := workflowJobBlock(string(raw), "fallback-deploy")
	if fallback == "" {
		t.Fatal("release-fast-path.yml: fallback-deploy job is missing")
	}
	if !strings.Contains(fallback, "uses: ./.github/workflows/deploy.yml") {
		t.Fatal("release-fast-path.yml: fallback-deploy must call deploy.yml")
	}
	if regexp.MustCompile(`(?m)^\s*secrets:`).MatchString(fallback) {
		t.Errorf("release-fast-path.yml: fallback-deploy must rely only on its automatic GITHUB_TOKEN and must not inherit or pass custom secrets:\n%s", fallback)
	}
}

func TestReleaseFallbackIsSynchronousAndArtifactsAreComplete(t *testing.T) {
	fastRaw, err := os.ReadFile(fastPathWorkflowRel)
	if err != nil {
		t.Fatalf("read release fast path: %v", err)
	}
	fast := string(fastRaw)

	for _, forbidden := range []string{"actions: write", "gh workflow run deploy.yml"} {
		if strings.Contains(fast, forbidden) {
			t.Errorf("release-fast-path.yml: asynchronous fallback marker %q must be removed", forbidden)
		}
	}
	for _, required := range []string{
		"uses: ./.github/workflows/deploy.yml",
		"needs: [retag-or-route, fallback-deploy]",
		"corescope-decrypt-linux-amd64",
		"corescope-decrypt-linux-arm64",
		"softprops/action-gh-release@v2",
	} {
		if !strings.Contains(fast, required) {
			t.Errorf("release-fast-path.yml: missing coherent release-path marker %q", required)
		}
	}

	deployRaw, err := os.ReadFile(deployWorkflowRel)
	if err != nil {
		t.Fatalf("read deploy workflow: %v", err)
	}
	deploy := string(deployRaw)
	if !regexp.MustCompile(`(?m)^\s{2}workflow_call:\s*$`).MatchString(deploy) {
		t.Error("deploy.yml: must support synchronous workflow_call fallback")
	}
	if strings.Contains(deploy, "softprops/action-gh-release") || strings.Contains(deploy, "corescope-decrypt-linux-") {
		t.Error("deploy.yml: release assets must have one owner in release-fast-path.yml")
	}
}

func TestReleaseGuideMatchesWorkflowOwnership(t *testing.T) {
	raw, err := os.ReadFile(releaseGuideRel)
	if err != nil {
		t.Fatalf("read release guide: %v", err)
	}
	src := string(raw)
	for _, required := range []string{"Release Fast-Path", "gh release edit"} {
		if !strings.Contains(src, required) {
			t.Errorf("release guide: missing current release-path instruction %q", required)
		}
	}
	for _, stale := range []string{
		"push:tag` event → CI/CD Pipeline reruns",
		"\ngh release create vX.Y.Z",
	} {
		if strings.Contains(src, stale) {
			t.Errorf("release guide: stale instruction %q", stale)
		}
	}
}

// workflowNamedStepBlock returns the step with the requested name.
func workflowNamedStepBlock(src, name string) string {
	lines := strings.Split(src, "\n")
	nameLine := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "- name: "+name {
			nameLine = i
			break
		}
	}
	if nameLine == -1 {
		return ""
	}
	end := len(lines)
	for i := nameLine + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "      - ") {
			end = i
			break
		}
	}
	return strings.Join(lines[nameLine:end], "\n")
}

// workflowStepBlock returns the step containing the requested id, stopping at
// the next step at the same indentation level.
func workflowStepBlock(src, id string) string {
	lines := strings.Split(src, "\n")
	idLine := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "id: "+id {
			idLine = i
			break
		}
	}
	if idLine == -1 {
		return ""
	}
	start := idLine
	for start >= 0 && !strings.HasPrefix(lines[start], "      - ") {
		start--
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := idLine + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "      - ") {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// workflowJobBlock returns one top-level job's YAML text, stopping at the next
// job at the same indentation level.
func workflowJobBlock(src, job string) string {
	lines := strings.Split(src, "\n")
	start := -1
	for i, line := range lines {
		if line == "  "+job+":" {
			start = i
			break
		}
	}
	if start == -1 {
		return ""
	}
	end := len(lines)
	jobHeader := regexp.MustCompile(`^  [A-Za-z0-9_-]+:\s*$`)
	for i := start + 1; i < len(lines); i++ {
		if jobHeader.MatchString(lines[i]) {
			end = i
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

// workflowRunBlocks returns command text from both scalar and single-line run
// steps. Expressions in env/with/if are intentionally outside this boundary;
// GitHub documents direct expression interpolation into a shell as unsafe.
func workflowRunBlocks(src string) []string {
	lines := strings.Split(src, "\n")
	var blocks []string
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "run:") {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " 	"))
		if rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "run:")); rest != "|" && rest != ">" && rest != "" {
			blocks = append(blocks, rest)
			continue
		}
		var block []string
		for _, candidate := range lines[i+1:] {
			candidateIndent := len(candidate) - len(strings.TrimLeft(candidate, " 	"))
			if strings.TrimSpace(candidate) != "" && candidateIndent <= indent {
				break
			}
			block = append(block, candidate)
		}
		blocks = append(blocks, strings.Join(block, "\n"))
	}
	return blocks
}
