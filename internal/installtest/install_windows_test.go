//go:build windows

package installtest

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const windowsTestToken = "ipgw-meta-install-test"

type windowsTestBundle struct {
	path    string
	sha256  string
	version string
}

type windowsFixture struct {
	root         string
	script       string
	home         string
	appData      string
	localAppData string
	install      string
	bin          string
	tokenFile    string
}

func newWindowsFixture(t *testing.T) *windowsFixture {
	t.Helper()
	rawRoot := t.TempDir()
	root, err := filepath.EvalSymlinks(rawRoot)
	if err != nil {
		t.Fatalf("resolve private test root: %v", err)
	}
	setPrivateDirectoryACL(t, root)
	f := &windowsFixture{
		root:         root,
		script:       filepath.Join(findWindowsRepoRoot(t), "install.ps1"),
		home:         filepath.Join(root, "home"),
		appData:      filepath.Join(root, "appdata"),
		localAppData: filepath.Join(root, "localappdata"),
		install:      filepath.Join(root, "install"),
		bin:          filepath.Join(root, "bin"),
		tokenFile:    filepath.Join(root, ".ipgw-install-test-token"),
	}
	for _, dir := range []string{f.home, f.appData, f.localAppData} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(f.tokenFile, []byte(windowsTestToken), 0o600); err != nil {
		t.Fatalf("write installer test token: %v", err)
	}
	return f
}

func findWindowsRepoRoot(t *testing.T) string {
	t.Helper()
	current, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolve installer test working directory: %v", err)
	}
	for {
		if info, statErr := os.Stat(filepath.Join(current, "install.ps1")); statErr == nil && !info.IsDir() {
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

func (f *windowsFixture) environment(overrides map[string]string) []string {
	dropped := map[string]bool{
		"APPDATA":                              true,
		"HOME":                                 true,
		"LOCALAPPDATA":                         true,
		"PSMODULEPATH":                         true,
		"USERPROFILE":                          true,
		"IPGW_VERSION":                         true,
		"IPGW_INSTALL_ROOT":                    true,
		"IPGW_BIN_DIR":                         true,
		"IPGW_INSTALL_TEST_ROOT":               true,
		"IPGW_INSTALL_TEST_TOKEN":              true,
		"IPGW_INSTALL_TEST_FAILPOINT":          true,
		"IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT": true,
	}
	env := make([]string, 0, len(os.Environ())+10)
	for _, item := range os.Environ() {
		name, _, _ := strings.Cut(item, "=")
		if !dropped[strings.ToUpper(name)] {
			env = append(env, item)
		}
	}
	values := map[string]string{
		"APPDATA":                 f.appData,
		"HOME":                    f.home,
		"LOCALAPPDATA":            f.localAppData,
		"USERPROFILE":             f.home,
		"IPGW_INSTALL_TEST_ROOT":  f.root,
		"IPGW_INSTALL_TEST_TOKEN": windowsTestToken,
		"HTTPS_PROXY":             "http://127.0.0.1:1",
		"HTTP_PROXY":              "http://127.0.0.1:1",
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

func (f *windowsFixture) run(t *testing.T, args []string, overrides map[string]string) (string, error) {
	t.Helper()
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatalf("locate Windows PowerShell: %v", err)
	}
	commandArgs := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", f.script}
	commandArgs = append(commandArgs, args...)
	cmd := exec.Command(powershell, commandArgs...)
	cmd.Dir = f.root
	cmd.Env = f.environment(overrides)
	output, runErr := cmd.CombinedOutput()
	return string(output), runErr
}

func (f *windowsFixture) installBundle(t *testing.T, bundle windowsTestBundle, failpoint, rollbackFailpoint string) (string, error) {
	t.Helper()
	overrides := map[string]string{}
	if failpoint != "" {
		overrides["IPGW_INSTALL_TEST_FAILPOINT"] = failpoint
	}
	if rollbackFailpoint != "" {
		overrides["IPGW_INSTALL_TEST_ROLLBACK_FAILPOINT"] = rollbackFailpoint
	}
	return f.installBundleWithOverrides(t, bundle, overrides)
}

func (f *windowsFixture) installBundleWithOverrides(t *testing.T, bundle windowsTestBundle, overrides map[string]string) (string, error) {
	t.Helper()
	return f.run(t, []string{
		"-BundlePath", bundle.path,
		"-BundleSha256", bundle.sha256,
		"-Version", bundle.version,
		"-InstallRoot", f.install,
		"-BinDir", f.bin,
	}, overrides)
}

var (
	helperBinaryMu    sync.Mutex
	helperBinaryBytes = map[string][]byte{}
)

func syntheticWindowsBinary(t *testing.T, version string) []byte {
	t.Helper()
	helperBinaryMu.Lock()
	defer helperBinaryMu.Unlock()
	if value, ok := helperBinaryBytes[version]; ok {
		return append([]byte(nil), value...)
	}
	dir, err := os.MkdirTemp("", "ipgw-installer-helper-")
	if err != nil {
		t.Fatalf("create helper build directory: %v", err)
	}
	defer os.RemoveAll(dir)
	source := []byte(`package main
import (
	"fmt"
	"os"
)
var version string
func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println(version)
		return
	}
	fmt.Println("synthetic ipgw installer entry")
}
`)
	sourcePath := filepath.Join(dir, "main.go")
	binaryPath := filepath.Join(dir, "entry.exe")
	if err := os.WriteFile(sourcePath, source, 0o600); err != nil {
		t.Fatalf("write helper source: %v", err)
	}
	cmd := exec.Command("go", "build", "-trimpath", "-o", binaryPath, "-ldflags=-s -w -X=main.version="+version, sourcePath)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build synthetic Windows entry: %v\n%s", err, output)
	}
	value, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read synthetic Windows entry: %v", err)
	}
	helperBinaryBytes[version] = append([]byte(nil), value...)
	return append([]byte(nil), value...)
}

func buildWindowsBundle(t *testing.T, root, version, extraName, specialName string) windowsTestBundle {
	t.Helper()
	target := runtime.GOOS + "-" + runtime.GOARCH
	entryNames := []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe"}
	entryBinary := syntheticWindowsBinary(t, version)
	contents := map[string][]byte{
		"ipgw.exe":              entryBinary,
		"ipgw-meta.exe":         entryBinary,
		"ipgw-legacy.exe":       entryBinary,
		"LICENSE":               []byte("synthetic installer test license\n"),
		"launcher-default.yaml": []byte("schema_version: 1\nmode: meta\ncohort: new-install\n"),
	}
	entryHash := map[string]string{}
	for _, name := range entryNames {
		entryHash[name] = hashWindowsBytes(contents[name])
	}
	contents["bundle-manifest.json"] = []byte(fmt.Sprintf(`{
  "schema_version": 1,
  "product": "ipgw-meta",
  "module": "github.com/UnbalancedCat/ipgw-meta",
  "version": "%s",
  "platform": "%s",
  "entries": [
    {"path": "ipgw.exe", "sha256": "%s", "size": %d},
    {"path": "ipgw-meta.exe", "sha256": "%s", "size": %d},
    {"path": "ipgw-legacy.exe", "sha256": "%s", "size": %d}
  ],
  "launcher_default": "meta",
  "layout": "versioned-bundle-v1",
  "self_update": false,
  "uninstall": {"remove_all_three_entries": true, "preserve_user_config": true}
}
`, version, target,
		entryHash["ipgw.exe"], len(contents["ipgw.exe"]),
		entryHash["ipgw-meta.exe"], len(contents["ipgw-meta.exe"]),
		entryHash["ipgw-legacy.exe"], len(contents["ipgw-legacy.exe"]),
	))
	checksumNames := []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe", "LICENSE", "launcher-default.yaml", "bundle-manifest.json"}
	var checksums strings.Builder
	for _, name := range checksumNames {
		fmt.Fprintf(&checksums, "%s  %s\n", hashWindowsBytes(contents[name]), name)
	}
	contents["SHA256SUMS"] = []byte(checksums.String())

	archivePath := filepath.Join(root, strings.ReplaceAll(version, "/", "-")+"-bundle-"+fmt.Sprint(time.Now().UnixNano())+".zip")
	archive, err := os.OpenFile(archivePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create synthetic Windows bundle: %v", err)
	}
	zipWriter := zip.NewWriter(archive)
	members := []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe", "LICENSE", "launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"}
	if extraName != "" {
		contents[extraName] = []byte("unexpected\n")
		members = append(members, extraName)
	}
	for _, name := range members {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.Modified = time.Unix(0, 0).UTC()
		mode := os.FileMode(0o644)
		if strings.HasSuffix(name, ".exe") {
			mode = 0o755
		}
		if name == specialName {
			mode = os.ModeSymlink | 0o777
		}
		header.SetMode(mode)
		writer, createErr := zipWriter.CreateHeader(header)
		if createErr != nil {
			t.Fatalf("create synthetic member %s: %v", name, createErr)
		}
		if _, writeErr := writer.Write(contents[name]); writeErr != nil {
			t.Fatalf("write synthetic member %s: %v", name, writeErr)
		}
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("close synthetic zip: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close synthetic Windows bundle: %v", err)
	}
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatalf("read synthetic Windows bundle: %v", err)
	}
	return windowsTestBundle{path: archivePath, sha256: hashWindowsBytes(archiveBytes), version: version}
}

func hashWindowsBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return fmt.Sprintf("%x", sum[:])
}

func TestWindowsOfflineFreshInstallAndUpgrade(t *testing.T) {
	f := newWindowsFixture(t)
	v1 := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
	if output, err := f.installBundle(t, v1, "", ""); err != nil {
		t.Fatalf("fresh offline install failed: %v\n%s", err, output)
	}
	assertWindowsInstalledVersion(t, f, v1.version)
	assertWindowsActiveVersion(t, f, v1.version)
	assertWindowsPrivateLayout(t, f)
	launcherPath := filepath.Join(f.appData, "ipgw-meta", "launcher.yaml")
	launcherBefore := mustReadWindowsFile(t, launcherPath)
	if !strings.Contains(string(launcherBefore), "mode: meta\ncohort: new-install\n") {
		t.Fatalf("fresh launcher does not use the specified default:\n%s", launcherBefore)
	}
	pathState := strings.TrimSpace(string(mustReadWindowsFile(t, filepath.Join(f.root, ".ipgw-user-path"))))
	if !strings.EqualFold(pathState, f.bin) {
		t.Fatalf("test PATH state is %q, want %q", pathState, f.bin)
	}
	assertNoWindowsTransactionArtifacts(t, f)

	oldActive := resolvedWindowsActive(t, f)
	v2 := buildWindowsBundle(t, f.root, "v1.1.0", "", "")
	if output, err := f.installBundle(t, v2, "", ""); err != nil {
		t.Fatalf("offline upgrade failed: %v\n%s", err, output)
	}
	assertWindowsInstalledVersion(t, f, v2.version)
	assertWindowsActiveVersion(t, f, v2.version)
	if newActive := resolvedWindowsActive(t, f); strings.EqualFold(newActive, oldActive) {
		t.Fatal("upgrade did not atomically select a new version directory")
	}
	launcherAfter := mustReadWindowsFile(t, launcherPath)
	if string(launcherAfter) != string(launcherBefore) {
		t.Fatal("upgrade changed the existing launcher selection")
	}
	assertNoWindowsTransactionArtifacts(t, f)
}

func TestWindowsForwardFailpointsRestorePreviousInstall(t *testing.T) {
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
			f := newWindowsFixture(t)
			v1 := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
			if output, err := f.installBundle(t, v1, "", ""); err != nil {
				t.Fatalf("prepare previous install: %v\n%s", err, output)
			}
			oldActive := resolvedWindowsActive(t, f)
			oldLauncher := mustReadWindowsFile(t, filepath.Join(f.appData, "ipgw-meta", "launcher.yaml"))
			v2 := buildWindowsBundle(t, f.root, "v1.1.0", "", "")
			if output, err := f.installBundle(t, v2, point, ""); err == nil {
				t.Fatalf("failpoint %s unexpectedly committed:\n%s", point, output)
			}
			if active := resolvedWindowsActive(t, f); !strings.EqualFold(active, oldActive) {
				t.Fatalf("failpoint %s did not restore the previous active junction", point)
			}
			if launcher := mustReadWindowsFile(t, filepath.Join(f.appData, "ipgw-meta", "launcher.yaml")); string(launcher) != string(oldLauncher) {
				t.Fatalf("failpoint %s changed the launcher selection", point)
			}
			assertWindowsInstalledVersion(t, f, v1.version)
			assertNoWindowsTransactionArtifacts(t, f)
		})
	}
}

func TestWindowsRollbackFailpointsPreserveRecoveryMaterials(t *testing.T) {
	tests := []struct {
		point         string
		entryVersion  string
		activeVersion string
	}{
		{point: "before_restore_entry_1", entryVersion: "v1.1.0", activeVersion: "v1.1.0"},
		{point: "before_restore_active", entryVersion: "v1.0.0", activeVersion: "v1.1.0"},
		{point: "before_remove_new_version", entryVersion: "v1.0.0", activeVersion: "v1.0.0"},
	}
	for _, test := range tests {
		t.Run(test.point, func(t *testing.T) {
			f := newWindowsFixture(t)
			v1 := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
			if output, err := f.installBundle(t, v1, "", ""); err != nil {
				t.Fatalf("prepare previous install: %v\n%s", err, output)
			}
			v2 := buildWindowsBundle(t, f.root, "v1.1.0", "", "")
			output, err := f.installBundle(t, v2, "before_commit", test.point)
			if err == nil {
				t.Fatalf("rollback failpoint %s unexpectedly committed:\n%s", test.point, output)
			}
			if !strings.Contains(strings.ToLower(output), "recovery materials remain") {
				t.Fatalf("rollback failpoint did not report preserved recovery materials:\n%s", output)
			}
			assertWindowsInstalledVersion(t, f, test.entryVersion)
			assertWindowsActiveVersion(t, f, test.activeVersion)
			assertWindowsRecoveryArtifacts(t, f)
		})
	}
}

func TestWindowsOfflineInputPathArchiveAndACLRejections(t *testing.T) {
	t.Run("missing checksum has no target side effects", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		output, err := f.run(t, []string{
			"-BundlePath", bundle.path,
			"-Version", bundle.version,
			"-InstallRoot", f.install,
			"-BinDir", f.bin,
		}, nil)
		if err == nil {
			t.Fatalf("missing checksum unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("wrong outer checksum", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		bundle.sha256 = strings.Repeat("0", 64)
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("wrong outer checksum unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("Everyone writable source", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		grantEveryoneWrite(t, bundle.path)
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("Everyone-writable source unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("UNC source", func(t *testing.T) {
		f := newWindowsFixture(t)
		output, err := f.run(t, []string{
			"-BundlePath", `\\localhost\C$\not-present.zip`,
			"-BundleSha256", strings.Repeat("0", 64),
			"-Version", "v1.0.0",
			"-InstallRoot", f.install,
			"-BinDir", f.bin,
		}, nil)
		if err == nil {
			t.Fatalf("UNC source unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("overlapping targets", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		overlapBin := filepath.Join(f.install, "bin")
		output, err := f.run(t, []string{
			"-BundlePath", bundle.path,
			"-BundleSha256", bundle.sha256,
			"-Version", bundle.version,
			"-InstallRoot", f.install,
			"-BinDir", overlapBin,
		}, nil)
		if err == nil {
			t.Fatalf("overlapping targets unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("junction target ancestor", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		realParent := filepath.Join(f.root, "redirect")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatalf("create redirect target: %v", err)
		}
		linkParent := filepath.Join(f.root, "linked-target")
		createJunction(t, linkParent, realParent)
		linkedInstall := filepath.Join(linkParent, "install")
		output, err := f.run(t, []string{
			"-BundlePath", bundle.path,
			"-BundleSha256", bundle.sha256,
			"-Version", bundle.version,
			"-InstallRoot", linkedInstall,
			"-BinDir", f.bin,
		}, nil)
		if err == nil {
			t.Fatalf("junction target ancestor unexpectedly succeeded:\n%s", output)
		}
		if _, err := os.Stat(filepath.Join(realParent, "install")); !os.IsNotExist(err) {
			t.Fatal("target ancestor rejection mutated the junction destination")
		}
	})

	t.Run("test token mismatch", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "")
		output, err := f.installBundleWithOverrides(t, bundle, map[string]string{
			"IPGW_INSTALL_TEST_TOKEN": "wrong-token",
		})
		if err == nil {
			t.Fatalf("mismatched test token unexpectedly succeeded:\n%s", output)
		}
		assertWindowsTargetsAbsent(t, f)
	})

	t.Run("unexpected archive member", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "unexpected-member", "")
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("archive with an extra member unexpectedly succeeded:\n%s", output)
		}
		assertWindowsNoCommittedInstall(t, f)
	})

	t.Run("link-typed archive member", func(t *testing.T) {
		f := newWindowsFixture(t)
		bundle := buildWindowsBundle(t, f.root, "v1.0.0", "", "LICENSE")
		if output, err := f.installBundle(t, bundle, "", ""); err == nil {
			t.Fatalf("archive with a link-typed member unexpectedly succeeded:\n%s", output)
		}
		assertWindowsNoCommittedInstall(t, f)
	})
}

func assertWindowsInstalledVersion(t *testing.T, f *windowsFixture, version string) {
	t.Helper()
	for _, name := range []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe"} {
		path := filepath.Join(f.bin, name)
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("stat published entry %s: %v", name, err)
		}
		if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("entry %s is not a regular published file: mode=%v", name, info.Mode())
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

func assertWindowsActiveVersion(t *testing.T, f *windowsFixture, version string) {
	t.Helper()
	path := filepath.Join(f.install, "active", "ipgw.exe")
	cmd := exec.Command(path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run active ipgw --version: %v\n%s", err, output)
	}
	if strings.TrimSpace(string(output)) != version {
		t.Fatalf("active entry reports %q, want %q", strings.TrimSpace(string(output)), version)
	}
}

func resolvedWindowsActive(t *testing.T, f *windowsFixture) string {
	t.Helper()
	value, err := os.Readlink(filepath.Join(f.install, "active"))
	if err != nil {
		t.Fatalf("read active junction: %v", err)
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(f.install, value)
	}
	return filepath.Clean(value)
}

func assertWindowsPrivateLayout(t *testing.T, f *windowsFixture) {
	t.Helper()
	activeTarget := resolvedWindowsActive(t, f)
	for _, path := range []string{
		f.install,
		filepath.Join(f.install, "versions"),
		activeTarget,
		f.bin,
		filepath.Join(f.appData, "ipgw-meta"),
	} {
		assertPrivateWindowsACL(t, path, true)
	}
	for _, name := range []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe", "LICENSE", "launcher-default.yaml", "bundle-manifest.json", "SHA256SUMS"} {
		assertPrivateWindowsACL(t, filepath.Join(activeTarget, name), false)
	}
	assertPrivateWindowsACL(t, filepath.Join(f.appData, "ipgw-meta", "launcher.yaml"), false)
}

func assertWindowsTargetsAbsent(t *testing.T, f *windowsFixture) {
	t.Helper()
	for _, path := range []string{f.install, f.bin, filepath.Join(f.appData, "ipgw-meta")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("rejected invocation left target %s: %v", path, err)
		}
	}
}

func assertWindowsNoCommittedInstall(t *testing.T, f *windowsFixture) {
	t.Helper()
	if _, err := os.Lstat(filepath.Join(f.install, "active")); !os.IsNotExist(err) {
		t.Fatalf("rejected archive left an active installation: %v", err)
	}
	for _, name := range []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe"} {
		if _, err := os.Lstat(filepath.Join(f.bin, name)); !os.IsNotExist(err) {
			t.Fatalf("rejected archive published %s: %v", name, err)
		}
	}
}

func assertNoWindowsTransactionArtifacts(t *testing.T, f *windowsFixture) {
	t.Helper()
	patterns := []string{
		filepath.Join(f.install, ".transaction.*"),
		filepath.Join(f.install, ".staging.*"),
		filepath.Join(f.install, ".active-next.*"),
		filepath.Join(f.bin, ".ipgw-meta-backup.*"),
		filepath.Join(f.bin, ".*.next.*"),
		filepath.Join(f.appData, "ipgw-meta", ".launcher.*"),
		filepath.Join(f.root, ".installer-tmp"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatalf("glob Windows transaction artifacts: %v", err)
		}
		if len(matches) != 0 {
			t.Fatalf("unexpected Windows transaction artifacts for %s: %v", pattern, matches)
		}
	}
}

func assertWindowsRecoveryArtifacts(t *testing.T, f *windowsFixture) {
	t.Helper()
	transactions, err := filepath.Glob(filepath.Join(f.install, ".transaction.*"))
	if err != nil || len(transactions) != 1 {
		t.Fatalf("recovery transaction count=%d err=%v", len(transactions), err)
	}
	assertPrivateWindowsACL(t, transactions[0], true)
	journal := filepath.Join(transactions[0], "journal")
	assertPrivateWindowsACL(t, journal, false)
	journalText := string(mustReadWindowsFile(t, journal))
	if !strings.HasPrefix(journalText, "schema_version=1\nphase=ready-to-commit\n") {
		t.Fatalf("unexpected restricted journal:\n%s", journalText)
	}
	if strings.Contains(strings.ToLower(journalText), strings.ToLower(f.root)) || strings.Contains(journalText, windowsTestToken) {
		t.Fatal("journal contains a test path or token instead of restricted state")
	}
	backups, err := filepath.Glob(filepath.Join(f.bin, ".ipgw-meta-backup.*"))
	if err != nil || len(backups) != 1 {
		t.Fatalf("recovery backup count=%d err=%v", len(backups), err)
	}
	assertPrivateWindowsACL(t, backups[0], true)
}

func setPrivateDirectoryACL(t *testing.T, path string) {
	t.Helper()
	script := `$p=$args[0]
$u=[Security.Principal.WindowsIdentity]::GetCurrent().User
$s=New-Object Security.Principal.SecurityIdentifier('S-1-5-18')
$a=New-Object Security.Principal.SecurityIdentifier('S-1-5-32-544')
$acl=New-Object Security.AccessControl.DirectorySecurity
$acl.SetOwner($u)
$acl.SetAccessRuleProtection($true,$false)
$inherit=[Security.AccessControl.InheritanceFlags]::ContainerInherit -bor [Security.AccessControl.InheritanceFlags]::ObjectInherit
foreach($sid in @($u,$s,$a)){
  $rule=New-Object Security.AccessControl.FileSystemAccessRule($sid,[Security.AccessControl.FileSystemRights]::FullControl,$inherit,[Security.AccessControl.PropagationFlags]::None,[Security.AccessControl.AccessControlType]::Allow)
  [void]$acl.AddAccessRule($rule)
}
[IO.Directory]::SetAccessControl($p,$acl)`
	runPowerShellHelper(t, script, path)
}

func assertPrivateWindowsACL(t *testing.T, path string, requireProtected bool) {
	t.Helper()
	protected := "False"
	if requireProtected {
		protected = "True"
	}
	script := `$p=$args[0]
$requireProtected=[bool]::Parse($args[1])
$u=[Security.Principal.WindowsIdentity]::GetCurrent().User.Value
$allowed=@($u,'S-1-5-18','S-1-5-32-544')
$sections=[Security.AccessControl.AccessControlSections]::Access
$acl=if([IO.Directory]::Exists($p)){[IO.Directory]::GetAccessControl($p,$sections)}else{[IO.File]::GetAccessControl($p,$sections)}
if($requireProtected -and -not $acl.AreAccessRulesProtected){throw 'ACL inheritance is not protected'}
foreach($rule in $acl.GetAccessRules($true,$true,[Security.Principal.SecurityIdentifier])){
  if($rule.AccessControlType -eq [Security.AccessControl.AccessControlType]::Allow -and $allowed -cnotcontains $rule.IdentityReference.Value){throw ('unexpected allow ACE: '+$rule.IdentityReference.Value)}
}`
	runPowerShellHelper(t, script, path, protected)
}

func grantEveryoneWrite(t *testing.T, path string) {
	t.Helper()
	script := `$p=$args[0]
$acl=[IO.File]::GetAccessControl($p,[Security.AccessControl.AccessControlSections]::Access)
$sid=New-Object Security.Principal.SecurityIdentifier('S-1-1-0')
$rule=New-Object Security.AccessControl.FileSystemAccessRule($sid,[Security.AccessControl.FileSystemRights]::Write,[Security.AccessControl.AccessControlType]::Allow)
[void]$acl.AddAccessRule($rule)
[IO.File]::SetAccessControl($p,$acl)`
	runPowerShellHelper(t, script, path)
}

func createJunction(t *testing.T, link, target string) {
	t.Helper()
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, target)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create test junction: %v\n%s", err, output)
	}
}

func runPowerShellHelper(t *testing.T, script string, args ...string) {
	t.Helper()
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Fatalf("locate Windows PowerShell helper: %v", err)
	}
	var command strings.Builder
	command.WriteString("& { ")
	command.WriteString(script)
	command.WriteString(" }")
	for _, arg := range args {
		command.WriteString(" '")
		command.WriteString(strings.ReplaceAll(arg, "'", "''"))
		command.WriteString("'")
	}
	cmd := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command.String())
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell helper failed: %v\n%s", err, output)
	}
}

func mustReadWindowsFile(t *testing.T, path string) []byte {
	t.Helper()
	value, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return value
}
