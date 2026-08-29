//go:build linux || windows

package livegate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type candidateTestFixture struct {
	directory    string
	candidate    string
	manifest     string
	explicitSHA  string
	candidateRaw []byte
	manifestRaw  []byte
	platform     Platform
}

func newCandidateTestFixture(t *testing.T) candidateTestFixture {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "private")
	preparePrivateTestDirectory(t, directory)
	platform := PlatformLinuxAMD64
	name := "ipgw-meta"
	if runtime.GOOS == "windows" {
		platform = PlatformWindowsAMD64
		name = "ipgw-meta.exe"
	}
	candidateRaw := []byte("synthetic candidate bytes\n")
	sum := sha256.Sum256(candidateRaw)
	explicitSHA := hex.EncodeToString(sum[:])
	manifestRaw := candidateTestManifest(candidateRaw)
	candidate := filepath.Join(directory, name)
	manifest := filepath.Join(directory, "candidate-manifest.json")
	writePrivateTestExecutableFile(t, candidate, candidateRaw)
	writePrivateTestReadableFile(t, manifest, manifestRaw)
	return candidateTestFixture{
		directory:    directory,
		candidate:    candidate,
		manifest:     manifest,
		explicitSHA:  explicitSHA,
		candidateRaw: candidateRaw,
		manifestRaw:  manifestRaw,
		platform:     platform,
	}
}

func candidateTestManifest(candidate []byte) []byte {
	sum := sha256.Sum256(candidate)
	targetSHA := hex.EncodeToString(sum[:])
	return []byte(fmt.Sprintf(
		"{\"schema_version\":1,\"plan_id\":\"IPGW-META-V1\",\"revision\":\"2026-08-28-r2\","+
			"\"version\":\"v1.0.0\",\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\","+
			"\"source_commit\":\"0123456789abcdef0123456789abcdef01234567\","+
			"\"live_gate_targets\":["+
			"{\"platform\":\"linux-amd64\",\"name\":\"ipgw-meta\",\"size\":%d,\"sha256\":\"%s\"},"+
			"{\"platform\":\"windows-amd64\",\"name\":\"ipgw-meta.exe\",\"size\":%d,\"sha256\":\"%s\"}]}",
		len(candidate), targetSHA, len(candidate), targetSHA,
	))
}

func selectedCandidateTestTarget(t *testing.T, object map[string]any, platform Platform) map[string]any {
	t.Helper()
	index := 0
	if platform == PlatformWindowsAMD64 {
		index = 1
	}
	return mustJSONArrayField(t, object, "live_gate_targets")[index].(map[string]any)
}

func TestVerifyCandidateBeforeAndAfterSyntheticBytes(t *testing.T) {
	fixture := newCandidateTestFixture(t)
	verified, err := VerifyCandidateBefore(fixture.candidate, fixture.manifest, fixture.explicitSHA, fixture.platform)
	if err != nil {
		t.Fatalf("VerifyCandidateBefore() error = %v", err)
	}
	if verified.CandidatePath != fixture.candidate ||
		verified.ManifestPath != fixture.manifest ||
		verified.Binding.Platform != fixture.platform ||
		verified.Binding.SHA256 != fixture.explicitSHA ||
		verified.Binding.Size != int64(len(fixture.candidateRaw)) {
		t.Fatalf("verified candidate = %#v", verified)
	}
	manifestDigest := sha256.Sum256(fixture.manifestRaw)
	if verified.Binding.CandidateSetSHA256 != hex.EncodeToString(manifestDigest[:]) {
		t.Fatal("manifest digest mismatch")
	}
	if err := verified.VerifyAfter(); err != nil {
		t.Fatalf("VerifyAfter() error = %v", err)
	}
}

func TestVerifyCandidateBeforeRejectsPathBasenameSizeAndHashMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, *candidateTestFixture) (string, string, string)
	}{
		{"relative_candidate", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			return filepath.Base(fixture.candidate), fixture.manifest, fixture.explicitSHA
		}},
		{"unclean_candidate", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			path := fixture.directory + string(os.PathSeparator) + "." + string(os.PathSeparator) + filepath.Base(fixture.candidate)
			return path, fixture.manifest, fixture.explicitSHA
		}},
		{"same_path", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			return fixture.manifest, fixture.manifest, fixture.explicitSHA
		}},
		{"wrong_basename", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			path := filepath.Join(fixture.directory, "wrong-candidate")
			writePrivateTestExecutableFile(t, path, fixture.candidateRaw)
			return path, fixture.manifest, fixture.explicitSHA
		}},
		{"manifest_size", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			object := mustJSONObject(t, fixture.manifestRaw)
			selectedCandidateTestTarget(t, object, fixture.platform)["size"] = len(fixture.candidateRaw) + 1
			writePrivateTestReadableFile(t, fixture.manifest, mustMarshalJSON(t, object))
			return fixture.candidate, fixture.manifest, fixture.explicitSHA
		}},
		{"manifest_hash", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			wrong := strings.Repeat("d", 64)
			object := mustJSONObject(t, fixture.manifestRaw)
			selectedCandidateTestTarget(t, object, fixture.platform)["sha256"] = wrong
			writePrivateTestReadableFile(t, fixture.manifest, mustMarshalJSON(t, object))
			return fixture.candidate, fixture.manifest, wrong
		}},
		{"explicit_hash", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			return fixture.candidate, fixture.manifest, strings.Repeat("d", 64)
		}},
		{"explicit_uppercase", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			return fixture.candidate, fixture.manifest, strings.ToUpper(fixture.explicitSHA)
		}},
		{"actual_hash", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			changed := append([]byte(nil), fixture.candidateRaw...)
			changed[0] ^= 1
			writePrivateTestExecutableFile(t, fixture.candidate, changed)
			return fixture.candidate, fixture.manifest, fixture.explicitSHA
		}},
		{"invalid_manifest", func(t *testing.T, fixture *candidateTestFixture) (string, string, string) {
			writePrivateTestReadableFile(t, fixture.manifest, []byte("{}"))
			return fixture.candidate, fixture.manifest, fixture.explicitSHA
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCandidateTestFixture(t)
			candidate, manifest, explicit := test.mutate(t, &fixture)
			_, err := VerifyCandidateBefore(candidate, manifest, explicit, fixture.platform)
			if !errors.Is(err, ErrCandidateSecurity) {
				t.Fatalf("error = %v, want ErrCandidateSecurity", err)
			}
		})
	}
}

func TestVerifyAfterDetectsCandidateManifestAndIdentityDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, candidateTestFixture)
	}{
		{"candidate_hash", func(t *testing.T, fixture candidateTestFixture) {
			changed := append([]byte(nil), fixture.candidateRaw...)
			changed[len(changed)-2] ^= 1
			writePrivateTestExecutableFile(t, fixture.candidate, changed)
		}},
		{"candidate_size", func(t *testing.T, fixture candidateTestFixture) {
			writePrivateTestExecutableFile(t, fixture.candidate, append(fixture.candidateRaw, 'x'))
		}},
		{"candidate_identity", func(t *testing.T, fixture candidateTestFixture) {
			replacePrivateTestFileIdentity(t, fixture.candidate, fixture.candidateRaw, true)
		}},
		{"candidate_missing", func(t *testing.T, fixture candidateTestFixture) {
			if err := os.Remove(fixture.candidate); err != nil {
				t.Fatalf("remove candidate: %v", err)
			}
		}},
		{"manifest_exact_bytes", func(t *testing.T, fixture candidateTestFixture) {
			writePrivateTestReadableFile(t, fixture.manifest, append(fixture.manifestRaw, ' '))
		}},
		{"manifest_projection", func(t *testing.T, fixture candidateTestFixture) {
			object := mustJSONObject(t, fixture.manifestRaw)
			object["future"] = true
			writePrivateTestReadableFile(t, fixture.manifest, mustMarshalJSON(t, object))
		}},
		{"manifest_identity", func(t *testing.T, fixture candidateTestFixture) {
			replacePrivateTestFileIdentity(t, fixture.manifest, fixture.manifestRaw, false)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCandidateTestFixture(t)
			verified, err := VerifyCandidateBefore(fixture.candidate, fixture.manifest, fixture.explicitSHA, fixture.platform)
			if err != nil {
				t.Fatalf("VerifyCandidateBefore() error = %v", err)
			}
			test.mutate(t, fixture)
			if err := verified.VerifyAfter(); !errors.Is(err, ErrCandidateDrift) {
				t.Fatalf("VerifyAfter() error = %v, want ErrCandidateDrift", err)
			}
		})
	}
}

func TestCandidateSecurityAndDriftErrorsDoNotLeakCanaries(t *testing.T) {
	const canary = "CANARY_password_cookie_path_920e?ticket=yes"
	fixture := newCandidateTestFixture(t)
	missing := filepath.Join(fixture.directory, canary)
	_, err := VerifyCandidateBefore(missing, fixture.manifest, canary, fixture.platform)
	if !errors.Is(err, ErrCandidateSecurity) {
		t.Fatalf("error = %v, want ErrCandidateSecurity", err)
	}
	assertErrorTreeDoesNotContain(t, err, canary)

	verified, err := VerifyCandidateBefore(fixture.candidate, fixture.manifest, fixture.explicitSHA, fixture.platform)
	if err != nil {
		t.Fatalf("VerifyCandidateBefore() error = %v", err)
	}
	if err := os.Remove(fixture.manifest); err != nil {
		t.Fatalf("remove manifest: %v", err)
	}
	verified.ManifestPath = filepath.Join(fixture.directory, canary)
	err = verified.VerifyAfter()
	if !errors.Is(err, ErrCandidateDrift) {
		t.Fatalf("error = %v, want ErrCandidateDrift", err)
	}
	assertErrorTreeDoesNotContain(t, err, canary)
}
func TestVerifyAfterDetectsCandidateExecutePermissionDrift(t *testing.T) {
	fixture := newCandidateTestFixture(t)
	verified, err := VerifyCandidateBefore(
		fixture.candidate,
		fixture.manifest,
		fixture.explicitSHA,
		fixture.platform,
	)
	if err != nil {
		t.Fatalf("VerifyCandidateBefore() error = %v", err)
	}

	removePrivateTestExecutePermission(t, fixture.candidate)
	if err := verified.VerifyAfter(); err != ErrCandidateDrift {
		t.Fatalf("VerifyAfter() error = %v, want fixed ErrCandidateDrift", err)
	}
}
func TestVerifyCandidateBeforeRejectsNonExecutableCandidate(t *testing.T) {
	fixture := newCandidateTestFixture(t)
	removePrivateTestExecutePermission(t, fixture.candidate)

	_, err := VerifyCandidateBefore(
		fixture.candidate,
		fixture.manifest,
		fixture.explicitSHA,
		fixture.platform,
	)
	if err != ErrCandidateSecurity {
		t.Fatalf("error = %v, want fixed ErrCandidateSecurity", err)
	}
}

func replacePrivateTestFileIdentity(t *testing.T, path string, data []byte, executable bool) {
	t.Helper()
	original, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat original identity fixture: %v", err)
	}
	replacement := path + ".identity-replacement"
	t.Cleanup(func() {
		_ = os.Remove(replacement)
	})
	if executable {
		writePrivateTestExecutableFile(t, replacement, data)
	} else {
		writePrivateTestReadableFile(t, replacement, data)
	}
	replacementInfo, err := os.Stat(replacement)
	if err != nil {
		t.Fatalf("stat replacement identity fixture: %v", err)
	}
	if os.SameFile(original, replacementInfo) {
		t.Fatal("replacement fixture unexpectedly reused the live original identity")
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove original identity fixture: %v", err)
	}
	if err := os.Rename(replacement, path); err != nil {
		t.Fatalf("install replacement identity fixture: %v", err)
	}
	final, err := os.Stat(path)
	if err != nil || os.SameFile(original, final) {
		t.Fatalf("replacement did not install a distinct identity: %v", err)
	}
}
