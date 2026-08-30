package workflowguard

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestPromotionWorkflowSafetyContract(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve promotion workflow guard source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	path := filepath.Join(root, ".github", "workflows", "promotion.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read promotion workflow: %v", err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("parse promotion workflow: %v", err)
	}

	if len(workflow.On) != 1 {
		t.Fatalf("promotion triggers = %v, want one push trigger", workflow.On)
	}
	push, ok := workflow.On["push"].(map[string]any)
	if !ok || len(push) != 1 {
		t.Fatalf("promotion push trigger = %v", workflow.On["push"])
	}
	tags, ok := push["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "v1.0.0" {
		t.Fatalf("promotion tags = %v, want exact v1.0.0", push["tags"])
	}
	if len(workflow.Permissions) != 0 {
		t.Fatalf("top-level permissions = %v, want empty", workflow.Permissions)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("promotion jobs = %v, want promote only", workflow.Jobs)
	}
	job, ok := workflow.Jobs["promote"]
	if !ok {
		t.Fatal("promotion workflow lacks promote job")
	}
	wantPermissions := map[string]string{
		"actions":      "read",
		"attestations": "read",
		"contents":     "write",
	}
	if !maps.Equal(job.Permissions, wantPermissions) {
		t.Fatalf("promotion permissions = %v, want %v", job.Permissions, wantPermissions)
	}

	actionPin := regexp.MustCompile(`^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+@[0-9a-f]{40}$`)
	uses := 0
	for _, step := range job.Steps {
		if step.Uses == "" {
			continue
		}
		uses++
		if !actionPin.MatchString(step.Uses) {
			t.Fatalf("promotion step %q lacks immutable action SHA: %q", step.Name, step.Uses)
		}
		if !strings.HasPrefix(step.Uses, "actions/checkout@") {
			t.Fatalf("promotion uses unexpected action %q", step.Uses)
		}
	}
	if uses != 1 {
		t.Fatalf("promotion action count = %d, want checkout only", uses)
	}

	createRun := stepRun(t, job, "Create one invisible no-clobber draft from exact Candidate bytes")
	wantAssets := []string{
		"SHA256SUMS",
		"install.ps1",
		"install.sh",
		"ipgw-meta-darwin-amd64.tar.gz",
		"ipgw-meta-darwin-arm64.tar.gz",
		"ipgw-meta-linux-amd64.tar.gz",
		"ipgw-meta-linux-arm64.tar.gz",
		"ipgw-meta-windows-amd64.zip",
		"ipgw-meta-windows-arm64.zip",
		"release-manifest.json",
	}
	if strings.Count(createRun, "$candidate_release/") != len(wantAssets) {
		t.Fatalf("draft asset path count is not exactly %d", len(wantAssets))
	}
	for _, name := range wantAssets {
		if strings.Count(createRun, `"$candidate_release/`+name+`"`) != 1 {
			t.Fatalf("draft asset %q is not selected exactly once", name)
		}
	}
	for _, forbiddenAsset := range []string{
		"candidate-manifest.json",
		"ipgw-live-gate-linux-amd64",
		"ipgw-live-gate-windows-amd64.exe",
	} {
		if strings.Contains(createRun, forbiddenAsset) {
			t.Fatalf("draft contains private or non-public asset %q", forbiddenAsset)
		}
	}
	for _, required := range []string{
		`gh release create "$VERSION"`,
		"--draft",
		"--latest=false",
		`--notes-file "$NOTES_PATH"`,
		`--title "IPGW-Meta v1.0.0"`,
		"--verify-tag",
	} {
		if !strings.Contains(createRun, required) {
			t.Fatalf("draft creation lacks %q", required)
		}
	}

	text := string(raw)
	for _, required := range []string{
		"make promotion-gate",
		`GITHUB_REF == refs/tags/v1.0.0`,
		`.object.type == "tag"`,
		`.verification.verified == true`,
		`.verification.reason == "valid"`,
		`release_status == 404`,
		"scripts/promotion.py lock",
		"scripts/promotion.py api",
		"scripts/promotion.py artifact",
		"scripts/promotion.py candidate",
		"scripts/promotion.py attestations",
		"scripts/promotion.py release",
		"--deny-self-hosted-runners",
		"--source-ref refs/heads/main",
		".github/workflows/candidate.yml",
		`gh release edit v1.0.0 --draft=false --latest=true --verify-tag`,
		"--expect-draft true",
		"--expect-draft false",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("promotion workflow lacks guard %q", required)
		}
	}
	if strings.Count(text, "make promotion-gate") != 5 {
		t.Fatalf("promotion gate count = %d, want initial, full rechecks, and both mutation boundaries", strings.Count(text, "make promotion-gate"))
	}
	if strings.Count(text, "gh release create ") != 1 || strings.Count(text, "gh release edit ") != 1 {
		t.Fatal("promotion must contain exactly one draft create and one publish mutation")
	}
	if strings.Count(text, "gh release download ") != 2 {
		t.Fatal("promotion must re-download draft and public assets exactly once each")
	}

	createIndex := strings.Index(text, "gh release create ")
	draftDownloadIndex := strings.Index(text, `gh release download "$VERSION"`)
	finalCheckIndex := strings.Index(text, "- name: Final identity and attestation recheck before publication")
	publishIndex := strings.Index(text, "gh release edit ")
	publicCheckIndex := strings.Index(text, "- name: Verify the public release and a fresh public re-download")
	order := []int{createIndex, draftDownloadIndex, finalCheckIndex, publishIndex, publicCheckIndex}
	if slices.Contains(order, -1) || !slices.IsSorted(order) {
		t.Fatalf("promotion mutation/verification order = %v", order)
	}

	for _, forbidden := range []string{
		"actions/setup-go@",
		"actions/setup-python@",
		"go build",
		"go run",
		"candidate-build",
		"make package",
		"scripts/release.sh",
		"gh release upload",
		"--clobber",
		"gh release delete",
		"git push",
		"git tag",
		"continue-on-error",
		"softprops/action-gh-release@",
		"actions/create-release@",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("promotion workflow contains forbidden operation %q", forbidden)
		}
	}
}
