package workflowguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMilestoneGateWiring(t *testing.T) {
	root, _, _ := loadCandidateWorkflow(t)

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makeText := string(makefile)
	for _, invocation := range []string{
		"@$(BASH) scripts/milestone-gate.sh candidate docs/upgrade/status.md",
		"@$(BASH) scripts/milestone-gate.sh promotion docs/upgrade/status.md",
		"@$(BASH) scripts/milestone-gate.sh release docs/upgrade/status.md",
	} {
		if strings.Count(makeText, invocation) != 1 {
			t.Fatalf("Makefile gate invocation count for %q is not exactly one", invocation)
		}
	}
	if strings.Count(makeText, "$(BASH) scripts/test-milestone-gate.sh") != 1 {
		t.Fatal("Makefile must run the milestone gate synthetic tests exactly once")
	}

	helper, err := os.ReadFile(filepath.Join(root, "scripts", "milestone-gate.sh"))
	if err != nil {
		t.Fatalf("read milestone gate helper: %v", err)
	}
	helperText := string(helper)
	if strings.Contains(helperText, "M0") {
		t.Fatal("milestone gate helper must not inspect M0")
	}
	for _, required := range []string{
		"require_milestone M1 complete",
		"require_milestone M2 complete",
		"require_milestone M3 in_progress",
		"require_milestone M3 complete",
	} {
		if strings.Count(helperText, required) != 1 {
			t.Fatalf("milestone gate helper requirement count for %q is not exactly one", required)
		}
	}

	ci, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatalf("read CI workflow: %v", err)
	}
	if strings.Count(string(ci), "run: bash scripts/test-milestone-gate.sh") != 1 {
		t.Fatal("CI must run the milestone gate synthetic tests exactly once")
	}
}
