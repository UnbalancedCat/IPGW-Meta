package workflowguard

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

type workflowFile struct {
	On          map[string]any         `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	Permissions map[string]string `yaml:"permissions"`
	Steps       []workflowStep    `yaml:"steps"`
}

type workflowStep struct {
	Name string         `yaml:"name"`
	Uses string         `yaml:"uses"`
	Run  string         `yaml:"run"`
	With map[string]any `yaml:"with"`
}

func TestCandidateWorkflowSafetyContract(t *testing.T) {
	root, raw, workflow := loadCandidateWorkflow(t)

	if len(workflow.On) != 1 {
		t.Fatalf("candidate workflow triggers = %v, want workflow_dispatch only", workflow.On)
	}
	if _, ok := workflow.On["workflow_dispatch"]; !ok {
		t.Fatal("candidate workflow must be manual workflow_dispatch only")
	}
	if len(workflow.Permissions) != 0 {
		t.Fatalf("top-level permissions = %v, want empty", workflow.Permissions)
	}

	wantJobs := []string{"attest", "build", "native-install", "preflight"}
	gotJobs := make([]string, 0, len(workflow.Jobs))
	for name := range workflow.Jobs {
		gotJobs = append(gotJobs, name)
	}
	slices.Sort(gotJobs)
	if !slices.Equal(gotJobs, wantJobs) {
		t.Fatalf("candidate jobs = %v, want %v", gotJobs, wantJobs)
	}
	wantPermissions := map[string]map[string]string{
		"preflight": {
			"checks":   "read",
			"contents": "read",
		},
		"build": {
			"actions":  "read",
			"contents": "read",
		},
		"native-install": {
			"actions":  "read",
			"contents": "read",
		},
		"attest": {
			"actions":           "read",
			"artifact-metadata": "write",
			"attestations":      "write",
			"contents":          "read",
			"id-token":          "write",
		},
	}
	for name, want := range wantPermissions {
		if got := workflow.Jobs[name].Permissions; !maps.Equal(got, want) {
			t.Fatalf("%s permissions = %v, want %v", name, got, want)
		}
	}

	actionPin := regexp.MustCompile(`^[0-9A-Za-z_.-]+/[0-9A-Za-z_.-]+@[0-9a-f]{40}$`)
	usesCount := 0
	attestCount := 0
	uploadCount := 0
	downloadCount := 0
	for jobName, job := range workflow.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" {
				continue
			}
			usesCount++
			if !actionPin.MatchString(step.Uses) {
				t.Fatalf("%s step %q does not use an immutable action SHA: %q", jobName, step.Name, step.Uses)
			}
			switch {
			case strings.HasPrefix(step.Uses, "actions/attest@"):
				attestCount++
			case strings.HasPrefix(step.Uses, "actions/upload-artifact@"):
				uploadCount++
				assertWith(t, step, "overwrite", "false")
				assertWith(t, step, "compression-level", "0")
				assertWith(t, step, "include-hidden-files", "false")
				assertWith(t, step, "retention-days", "90")
			case strings.HasPrefix(step.Uses, "actions/download-artifact@"):
				downloadCount++
				assertWith(t, step, "artifact-ids", "${{ needs.build.outputs.artifact_id }}")
				if _, ok := step.With["name"]; ok {
					t.Fatalf("download step %q may not select artifact by name", step.Name)
				}
			}
		}
	}
	if usesCount != 12 || attestCount != 2 || uploadCount != 1 || downloadCount != 2 {
		t.Fatalf("action counts uses/attest/upload/download = %d/%d/%d/%d", usesCount, attestCount, uploadCount, downloadCount)
	}

	attestJob := workflow.Jobs["attest"]
	publicRun := stepRun(t, attestJob, "Reverify candidate and prepare exact public subject set")
	fields := strings.Fields(publicRun)
	shaIndex := slices.Index(fields, "sha256sum")
	if shaIndex < 0 {
		t.Fatal("public subject step does not invoke sha256sum")
	}
	var subjects []string
	for _, field := range fields[shaIndex+1:] {
		if field == ")" {
			break
		}
		subjects = append(subjects, field)
	}
	wantSubjects := []string{
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
	if !reflect.DeepEqual(subjects, wantSubjects) {
		t.Fatalf("public attestation subjects = %v, want %v", subjects, wantSubjects)
	}

	summaryRun := stepRun(t, attestJob, "Record verified candidate identity")
	for _, required := range []string{
		"$GITHUB_STEP_SUMMARY",
		"$CANDIDATE_ATTESTATION_ID",
		"$PUBLIC_ATTESTATION_ID",
		"$ARTIFACT_ID",
		"$ARTIFACT_DIGEST",
		"$CANDIDATE_SET_SHA256",
		"$BUILD_INPUT_SHA256",
		"$RELEASE_MANIFEST_SHA256",
		"$RUNNER_TEMP/ipgw-public-subjects.sha256",
	} {
		if !strings.Contains(summaryRun, required) {
			t.Fatalf("candidate summary lacks verified identity %q", required)
		}
	}
	text := string(raw)
	for _, required := range []string{
		"actions/runs/$GITHUB_RUN_ID/attempts/$GITHUB_RUN_ATTEMPT",
		".verificationResult.signature.certificate.runInvocationURI",
		".workflow_run.repository_id == $repository_id",
		".workflow_run.head_repository_id == $repository_id",
		`.path == ".github/workflows/candidate.yml"`,
		"make candidate-gate",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("candidate workflow lacks guard %q", required)
		}
	}
	for _, forbidden := range []string{
		"contents: write",
		"gh release ",
		"git push ",
		"git tag ",
		"actions/create-release@",
		"softprops/action-gh-release@",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("candidate workflow contains forbidden release mutation %q", forbidden)
		}
	}

	releaseWorkflow := filepath.Join(root, ".github", "workflows", "release.yml")
	if _, err := os.Lstat(releaseWorkflow); err == nil {
		t.Fatal("legacy tag-triggered release workflow must remain absent")
	} else if !os.IsNotExist(err) {
		t.Fatalf("inspect legacy release workflow: %v", err)
	}
}

func loadCandidateWorkflow(t *testing.T) (string, []byte, workflowFile) {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve workflow guard source")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", ".."))
	path := filepath.Join(root, ".github", "workflows", "candidate.yml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read candidate workflow: %v", err)
	}
	var workflow workflowFile
	if err := yaml.Unmarshal(raw, &workflow); err != nil {
		t.Fatalf("parse candidate workflow: %v", err)
	}
	return root, raw, workflow
}

func assertWith(t *testing.T, step workflowStep, name, want string) {
	t.Helper()
	got, ok := step.With[name]
	if !ok || fmt.Sprint(got) != want {
		t.Fatalf("step %q with.%s = %v, want %s", step.Name, name, got, want)
	}
}

func stepRun(t *testing.T, job workflowJob, name string) string {
	t.Helper()
	for _, step := range job.Steps {
		if step.Name == name {
			return step.Run
		}
	}
	t.Fatalf("workflow step %q not found", name)
	return ""
}
