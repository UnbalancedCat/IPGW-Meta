//go:build linux || darwin

package installtest

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const testToken = "ipgw-meta-install-test"

type testBundle struct {
	path    string
	sha256  string
	version string
}

type unixFixture struct {
	root      string
	script    string
	home      string
	config    string
	install   string
	bin       string
	stubDir   string
	tokenFile string
}

func newUnixFixture(t *testing.T) *unixFixture {
	t.Helper()
	rawRoot := t.TempDir()
	root, err := filepath.EvalSymlinks(rawRoot)
	if err != nil {
		t.Fatalf("resolve private test root: %v", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatalf("make test root private: %v", err)
	}
	repoRoot := findRepoRoot(t)
	home := filepath.Join(root, "home")
	config := filepath.Join(root, "config")
	if runtime.GOOS == "darwin" {
		config = filepath.Join(home, "Library", "Application Support")
	}
	f := &unixFixture{
		root:      root,
		script:    filepath.Join(repoRoot, "install.sh"),
		home:      home,
		config:    config,
		install:   filepath.Join(root, "install"),
		bin:       filepath.Join(root, "bin"),
		stubDir:   filepath.Join(root, "stub-bin"),
		tokenFile: filepath.Join(root, ".ipgw-install-test-token"),
	}
	for _, dir := range []string{f.home, f.stubDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(f.tokenFile, []byte(testToken), 0o600); err != nil {
		t.Fatalf("write installer test token: %v", err)
	}
	curlStub := filepath.Join(f.stubDir, "curl")
	if err := os.WriteFile(curlStub, []byte("#!/bin/sh\n: > curl-called\nexit 97\n"), 0o700); err != nil {
		t.Fatalf("write curl tripwire: %v", err)
	}
	return f
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve installer test working directory: %v", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, "install.sh")); statErr == nil && !info.IsDir() {
			if moduleInfo, moduleErr := os.Stat(filepath.Join(current, "go.mod")); moduleErr == nil && !moduleInfo.IsDir() {
				return current
			}
		}
		parent := filepath.Dir(current)
		if parent == current {
			t.Fatal("could not locate repository root from installer test working directory")
		}
		current = parent
	}
}

func (f *unixFixture) environment(overrides map[string]string) []string {
	dropped := map[string]bool{
		"HOME":                                 true,
		"PATH":                                 true,
		"XDG_CONFIG_HOME":                      true,
		"IPGW_VERSION":                         true,
		"IPGW_INSTALL_ROOT":                    true,
		"IPGW_BIN_DIR":                         true,
		"IPGW_INSTALL_TEST_ROOT":               true,
		"IPGW_INSTALL_TEST_TOKEN":              true,
		"IPGW_INSTALL_TEST_FAILPOINT":          true,
		"IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT": true,
	}
	env := make([]string, 0, len(os.Environ())+8)
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if !dropped[name] {
			env = append(env, item)
		}
	}
	values := map[string]string{
		"HOME":                    f.home,
		"PATH":                    f.stubDir + ":/usr/bin:/bin:/usr/sbin:/sbin",
		"XDG_CONFIG_HOME":         f.config,
		"IPGW_INSTALL_TEST_ROOT":  f.root,
		"IPGW_INSTALL_TEST_TOKEN": testToken,
	}
	for name, value := range overrides {
		values[name] = value
	}
	for name, value := range values {
		if value != "" {
			env = append(env, name+"="+value)
		}
	}
	return env
}

func (f *unixFixture) run(t *testing.T, args []string, overrides map[string]string) (string, error) {
	t.Helper()
	cmd := exec.Command("bash", append([]string{f.script}, args...)...)
	cmd.Dir = f.root
	cmd.Env = f.environment(overrides)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func (f *unixFixture) installBundle(t *testing.T, bundle testBundle, failpoint, rollbackFailpoint string) (string, error) {
	t.Helper()
	overrides := map[string]string{}
	if failpoint != "" {
		overrides["IPGW_INSTALL_TEST_FAILPOINT"] = failpoint
	}
	if rollbackFailpoint != "" {
		overrides["IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT"] = rollbackFailpoint
	}
	return f.run(t, []string{
		"--bundle", bundle.path,
		"--bundle-sha256", bundle.sha256,
		"--version", bundle.version,
		"--install-root", f.install,
		"--bin-dir", f.bin,
	}, overrides)
}

func buildBundle(t *testing.T, root, version, extraName string) testBundle {
	t.Helper()
	target := runtime.GOOS + "-" + runtime.GOARCH
	entryNames := []string{"ipgw", "ipgw-meta", "ipgw-legacy"}
	contents := make(map[string][]byte)
	for _, name := range entryNames {
		contents[name] = []byte(fmt.Sprintf(
			"#!/bin/sh\nif [ \"${1:-}\" = \"--version\" ]; then printf '%%s\\n' '%s'; exit 0; fi\nprintf '%%s\\n' '%s %s'\n",
			version, name, version,
		))
	}
	contents["LICENSE"] = []byte("synthetic installer test license\n")
	contents["launcher-default.yaml"] = []byte("schema_version: 1\nmode: meta\ncohort: new-install\n")

	entryHash := make(map[string]string)
	for _, name := range entryNames {
		entryHash[name] = hashBytes(contents[name])
	}
	contents["bundle-manifest.json"] = []byte(fmt.Sprintf(`{
  "schema_version": 1,
  "product": "ipgw-meta",
  "module": "github.com/UnbalancedCat/ipgw-meta",
  "version": "%s",
  "platform": "%s",
  "entries": [
    {"path": "ipgw", "sha256": "%s", "size": %d},
    {"path": "ipgw-meta", "sha256": "%s", "size": %d},
    {"path": "ipgw-legacy", "sha256": "%s", "size": %d}
  ],
  "launcher_default": "meta",
  "layout": "versioned-bundle-v1",
  "self_update": false,
  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}
}
`, version, target,
		entryHash["ipgw"], len(contents["ipgw"]),
		entryHash["ipgw-meta"], len(contents["ipgw-meta"]),
		entryHash["ipgw-legacy"], len(contents["ipgw-legacy"]),
	))
	checksumNames := []string{"ipgw", "ipgw-meta", "ipgw-legacy", "LICENSE", "launcher-default.yaml", "bundle-manifest.json"}
	var checksums strings.Builder
	for _, name := range checksumNames {
		fmt.Fprintf(&checksums, "%s  %s\n", hashBytes(contents[name]), name)
	}
	contents["SHA256SUMS"] = []byte(checksums.String())

	archivePath := filepath.Join(root, strings.ReplaceAll(version, "/", "-")+"-bundle.tar.gz")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create synthetic bundle: %v", err)
	}
	gzipWriter := gzip.NewWriter(archive)
	gzipWriter.ModTime = time.Unix(0, 0)
	tarWriter := tar.NewWriter(gzipWriter)
	memberNames := []string{"ipgw", "ipgw-meta", "ipgw-legacy", "LICENSE", "launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"}
	if extraName != "" {
		contents[extraName] = []byte("unexpected\n")
		memberNames = append(memberNames, extraName)
	}
	for _, name := range memberNames {
		mode := int64(0o644)
		if name == "ipgw" || name == "ipgw-meta" || name == "ipgw-legacy" {
			mode = 0o755
		}
		header := &tar.Header{
			Name:     name,
			Mode:     mode,
			Size:     int64(len(contents[name])),
			Typeflag: tar.TypeReg,
			ModTime:  time.Unix(0, 0),
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			t.Fatalf("write synthetic member header %s: %v", name, err)
		}
		if _, err := tarWriter.Write(contents[name]); err != nil {
			t.Fatalf("write synthetic member %s: %v", name, err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("close synthetic tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close synthetic gzip: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close synthetic bundle: %v", err)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read synthetic bundle: %v", err)
	}
	return testBundle{path: archivePath, sha256: hashBytes(archiveBytes), version: version}
}

func hashBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return fmt.Sprintf("%x", sum[:])
}

func TestOfflineFreshInstallAndUpgrade(t *testing.T) {
	f := newUnixFixture(t)
	v1 := buildBundle(t, f.root, "v1.0.0", "")
	if output, err := f.installBundle(t, v1, "", ""); err != nil {
		t.Fatalf("fresh offline install failed: %v\n%s", err, output)
	}
	assertInstalledVersion(t, f, v1.version)
	assertInstalledModes(t, f)
	launcherBefore := mustReadFile(t, filepath.Join(f.config, "ipgw-meta", "launcher.yaml"))
	if !strings.Contains(string(launcherBefore), "mode: meta\ncohort: new-install\n") {
		t.Fatalf("fresh launcher does not use the specified default:\n%s", launcherBefore)
	}
	assertNoTransactionArtifacts(t, f)
	if _, err := os.Stat(filepath.Join(f.root, "curl-called")); !os.IsNotExist(err) {
		t.Fatal("offline install invoked the curl tripwire")
	}

	oldActive := mustReadlink(t, filepath.Join(f.install, "active"))
	v2 := buildBundle(t, f.root, "v1.1.0", "")
	if output, err := f.installBundle(t, v2, "", ""); err != nil {
		t.Fatalf("offline upgrade failed: %v\n%s", err, output)
	}
	assertInstalledVersion(t, f, v2.version)
	if newActive := mustReadlink(t, filepath.Join(f.install, "active")); newActive == oldActive {
		t.Fatal("upgrade did not atomically select a new version directory")
	}
	launcherAfter := mustReadFile(t, filepath.Join(f.config, "ipgw-meta", "launcher.yaml"))
	if string(launcherAfter) != string(launcherBefore) {
		t.Fatal("upgrade changed the existing launcher selection")
	}
	assertNoTransactionArtifacts(t, f)
}

func TestForwardFailpointsRestorePreviousInstall(t *testing.T) {
	points := []string{
		"after_verified_stage",
		"after_version_publish",
		"after_old_active_detach",
		"after_active_switch",
		"after_entry_1",
		"after_entry_2",
		"after_launcher_publish",
		"after_path_update",
		"before_commit",
	}
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			f := newUnixFixture(t)
			v1 := buildBundle(t, f.root, "v1.0.0", "")
			if output, err := f.installBundle(t, v1, "", ""); err != nil {
				t.Fatalf("prepare previous install: %v\n%s", err, output)
			}
			oldActive := mustReadlink(t, filepath.Join(f.install, "active"))
			oldLauncher := mustReadFile(t, filepath.Join(f.config, "ipgw-meta", "launcher.yaml"))
			v2 := buildBundle(t, f.root, "v1.1.0", "")
			if output, err := f.installBundle(t, v2, point, ""); err == nil {
				t.Fatalf("failpoint %s unexpectedly committed:\n%s", point, output)
			}
			if active := mustReadlink(t, filepath.Join(f.install, "active")); active != oldActive {
				t.Fatalf("failpoint %s did not restore the previous active link", point)
			}
			if launcher := mustReadFile(t, filepath.Join(f.config, "ipgw-meta", "launcher.yaml")); string(launcher) != string(oldLauncher) {
				t.Fatalf("failpoint %s changed the launcher selection", point)
			}
			assertInstalledVersion(t, f, v1.version)
			assertNoTransactionArtifacts(t, f)
		})
	}
}

func TestRollbackFailpointsPreserveRecoveryMaterials(t *testing.T) {
	tests := []struct {
		point           string
		expectedVersion string
	}{
		{point: "before_restore_entry_1", expectedVersion: "v1.1.0"},
		{point: "before_restore_active", expectedVersion: "v1.1.0"},
		{point: "before_remove_new_version", expectedVersion: "v1.0.0"},
	}
	for _, test := range tests {
		t.Run(test.point, func(t *testing.T) {
			f := newUnixFixture(t)
			v1 := buildBundle(t, f.root, "v1.0.0", "")
			if output, err := f.installBundle(t, v1, "", ""); err != nil {
				t.Fatalf("prepare previous install: %v\n%s", err, output)
			}
			v2 := buildBundle(t, f.root, "v1.1.0", "")
			output, err := f.installBundle(t, v2, "before_commit", test.point)
			if err == nil {
				t.Fatalf("rollback failpoint %s unexpectedly committed:\n%s", test.point, output)
			}
			if !strings.Contains(output, "recovery materials remain") {
				t.Fatalf("rollback failpoint did not report preserved recovery materials:\n%s", output)
			}
			assertInstalledVersion(t, f, test.expectedVersion)
			assertRecoveryArtifacts(t, f)
		})
	}
}

func TestOfflineInputPathAndPermissionRejections(t *testing.T) {
	t.Run("missing checksum has no target side effects", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		output, err := f.run(t, []string{
			"--bundle", bundle.path,
			"--version", bundle.version,
			"--install-root", f.install,
			"--bin-dir", f.bin,
		}, nil)
		if err == nil {
			t.Fatalf("missing checksum unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("wrong outer checksum", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		bundle.sha256 = strings.Repeat("0", 64)
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("wrong outer checksum unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("group writable source", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		if err := os.Chmod(bundle.path, 0o660); err != nil {
			t.Fatalf("make source group writable: %v", err)
		}
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("group-writable source unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("symbolic link source", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		linkPath := filepath.Join(f.root, "bundle-link.tar.gz")
		if err := os.Symlink(bundle.path, linkPath); err != nil {
			t.Fatalf("create source symlink: %v", err)
		}
		bundle.path = linkPath
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("symbolic-link source unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("overlapping targets", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		overlapBin := filepath.Join(f.install, "bin")
		output, err := f.run(t, []string{
			"--bundle", bundle.path,
			"--bundle-sha256", bundle.sha256,
			"--version", bundle.version,
			"--install-root", f.install,
			"--bin-dir", overlapBin,
		}, nil)
		if err == nil {
			t.Fatalf("overlapping targets unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("symbolic link target ancestor", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		realParent := filepath.Join(f.root, "redirect")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatalf("create redirect target: %v", err)
		}
		linkParent := filepath.Join(f.root, "linked-target")
		if err := os.Symlink(realParent, linkParent); err != nil {
			t.Fatalf("create target ancestor symlink: %v", err)
		}
		linkedInstall := filepath.Join(linkParent, "install")
		output, err := f.run(t, []string{
			"--bundle", bundle.path,
			"--bundle-sha256", bundle.sha256,
			"--version", bundle.version,
			"--install-root", linkedInstall,
			"--bin-dir", f.bin,
		}, nil)
		if err == nil {
			t.Fatalf("symbolic-link target ancestor unexpectedly succeeded:\n%s", output)
		}
		if _, err := os.Stat(filepath.Join(realParent, "install")); !os.IsNotExist(err) {
			t.Fatal("target ancestor rejection mutated the symlink destination")
		}
	})

	t.Run("test token mismatch", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "")
		output, err := f.installBundleWithOverrides(t, bundle, map[string]string{
			"IPGW_INSTALL_TEST_TOKEN": "wrong-token",
		})
		if err == nil {
			t.Fatalf("mismatched test token unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})

	t.Run("unexpected archive member", func(t *testing.T) {
		f := newUnixFixture(t)
		bundle := buildBundle(t, f.root, "v1.0.0", "unexpected-member")
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("archive with an extra member unexpectedly succeeded:\n%s", output)
		}
		assertTargetsAbsent(t, f)
	})
}

func (f *unixFixture) installBundleWithOverrides(t *testing.T, bundle testBundle, overrides map[string]string) (string, error) {
	t.Helper()
	return f.run(t, []string{
		"--bundle", bundle.path,
		"--bundle-sha256", bundle.sha256,
		"--version", bundle.version,
		"--install-root", f.install,
		"--bin-dir", f.bin,
	}, overrides)
}

func assertInstalledVersion(t *testing.T, f *unixFixture, version string) {
	t.Helper()
	for _, name := range []string{"ipgw", "ipgw-meta", "ipgw-legacy"} {
		path := filepath.Join(f.bin, name)
		if info, err := os.Lstat(path); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("entry %s is not a published symbolic link: info=%v err=%v", name, info, err)
		}
		cmd := exec.Command(path, "--version")
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %s --version: %v\n%s", name, err, output)
		}
		if strings.TrimSpace(string(output)) != version {
			t.Fatalf("entry %s reports %q, want %q", name, strings.TrimSpace(string(output)), version)
		}
	}
}

func assertInstalledModes(t *testing.T, f *unixFixture) {
	t.Helper()
	assertMode(t, f.install, 0o755)
	assertMode(t, filepath.Join(f.install, "versions"), 0o755)
	assertMode(t, f.bin, 0o755)
	assertMode(t, filepath.Join(f.config, "ipgw-meta"), 0o755)
	active := mustReadlink(t, filepath.Join(f.install, "active"))
	versionDir := filepath.Join(f.install, filepath.FromSlash(active))
	assertMode(t, versionDir, 0o755)
	for _, name := range []string{"ipgw", "ipgw-meta", "ipgw-legacy"} {
		assertMode(t, filepath.Join(versionDir, name), 0o755)
	}
	for _, name := range []string{"LICENSE", "launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"} {
		assertMode(t, filepath.Join(versionDir, name), 0o644)
	}
	assertMode(t, filepath.Join(f.config, "ipgw-meta", "launcher.yaml"), 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %s is %04o, want %04o", path, got, want)
	}
}

func assertTargetsAbsent(t *testing.T, f *unixFixture) {
	t.Helper()
	for _, path := range []string{f.install, f.bin, f.config} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected invocation left target %s: %v", path, err)
		}
	}
}

func assertNoTransactionArtifacts(t *testing.T, f *unixFixture) {
	t.Helper()
	patterns := []string{
		filepath.Join(f.install, ".transaction.*"),
		filepath.Join(f.install, ".staging.*"),
		filepath.Join(f.install, ".active-next.*"),
		filepath.Join(f.bin, ".ipgw-meta-backup.*"),
		filepath.Join(f.bin, ".*.next.*"),
		filepath.Join(f.config, "ipgw-meta", ".launcher.*"),
		filepath.Join(f.root, ".installer-tmp"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob transaction artifacts: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("unexpected transaction artifacts for %s: %v", pattern, matches)
		}
	}
}

func assertRecoveryArtifacts(t *testing.T, f *unixFixture) {
	t.Helper()
	transactions, err := filepath.Glob(filepath.Join(f.install, ".transaction.*"))
	if err != nil || len(transactions) != 1 {
		t.Fatalf("recovery transaction count=%d err=%v", len(transactions), err)
	}
	assertMode(t, transactions[0], 0o700)
	journal := filepath.Join(transactions[0], "journal")
	assertMode(t, journal, 0o600)
	journalText := string(mustReadFile(t, journal))
	if !strings.HasPrefix(journalText, "schema_version=1\nphase=ready-to-commit\n") {
		t.Fatalf("unexpected restricted journal:\n%s", journalText)
	}
	if strings.Contains(journalText, f.root) || strings.Contains(journalText, testToken) {
		t.Fatal("journal contains a test path or token instead of restricted state")
	}
	backups, err := filepath.Glob(filepath.Join(f.bin, ".ipgw-meta-backup.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("recovery backup count=%d err=%v", len(backups), err)
	}
	assertMode(t, backups[0], 0o700)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return value
}

func mustReadlink(t *testing.T, path string) string {
	t.Helper()
	value, err := os.Readlink(path)
	if err != nil {
		t.Fatalf("read link %s: %v", path, err)
	}
	return value
}
