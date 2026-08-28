//go:build linux || darwin

package config

import (
	"os"
	"reflect"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestAbsoluteCredentialPathPartsUsesLexicalClean(t *testing.T) {
	absPath, parts, err := absoluteCredentialPathParts("/var/../private/var/./tmp/credential.txt")
	if err != nil {
		t.Fatalf("absoluteCredentialPathParts: %v", err)
	}
	if absPath != "/private/var/tmp/credential.txt" {
		t.Fatalf("absolute path = %q", absPath)
	}
	wantParts := []string{"private", "var", "tmp", "credential.txt"}
	if !reflect.DeepEqual(parts, wantParts) {
		t.Fatalf("parts = %#v, want %#v", parts, wantParts)
	}
}

func TestSelectTrustedDarwinVarAlias(t *testing.T) {
	tests := []struct {
		name  string
		goos  string
		parts []string
		want  bool
	}{
		{name: "darwin top-level var parent", goos: "darwin", parts: []string{"var", "folders", "credential.txt"}, want: true},
		{name: "linux", goos: "linux", parts: []string{"var", "tmp", "credential.txt"}},
		{name: "nested var", goos: "darwin", parts: []string{"private", "var", "credential.txt"}},
		{name: "tmp", goos: "darwin", parts: []string{"tmp", "credential.txt"}},
		{name: "etc", goos: "darwin", parts: []string{"etc", "credential.txt"}},
		{name: "home", goos: "darwin", parts: []string{"home", "credential.txt"}},
		{name: "var is final object", goos: "darwin", parts: []string{"var"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := selectTrustedDarwinVarAlias(test.goos, test.parts); got != test.want {
				t.Fatalf("selectTrustedDarwinVarAlias() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestTrustedDarwinVarAliasPolicy(t *testing.T) {
	good := trustedSystemAliasSnapshot{target: trustedDarwinVarTarget}
	good.stat.Dev = 1
	good.stat.Ino = 2
	good.stat.Mode = unix.S_IFLNK | 0o777
	good.stat.Uid = 0

	notLink := good
	notLink.stat.Mode = unix.S_IFDIR | 0o755
	wrongOwner := good
	wrongOwner.stat.Uid = 501
	changedInode := good
	changedInode.stat.Ino++
	changedDevice := good
	changedDevice.stat.Dev++
	changedMode := good
	changedMode.stat.Mode = unix.S_IFLNK | 0o755

	tests := []struct {
		name      string
		goos      string
		index     int
		component string
		before    trustedSystemAliasSnapshot
		after     trustedSystemAliasSnapshot
		want      bool
	}{
		{name: "exact tuple", goos: "darwin", component: "var", before: good, after: good, want: true},
		{name: "linux", goos: "linux", component: "var", before: good, after: good},
		{name: "not first component", goos: "darwin", index: 1, component: "var", before: good, after: good},
		{name: "wrong component", goos: "darwin", component: "tmp", before: good, after: good},
		{name: "not symlink before", goos: "darwin", component: "var", before: notLink, after: good},
		{name: "not symlink after", goos: "darwin", component: "var", before: good, after: notLink},
		{name: "wrong owner before", goos: "darwin", component: "var", before: wrongOwner, after: good},
		{name: "wrong owner after", goos: "darwin", component: "var", before: good, after: wrongOwner},
		{name: "absolute target", goos: "darwin", component: "var", before: aliasSnapshotWithTarget(good, "/private/var"), after: good},
		{name: "dot target", goos: "darwin", component: "var", before: aliasSnapshotWithTarget(good, "./private/var"), after: good},
		{name: "double separator", goos: "darwin", component: "var", before: aliasSnapshotWithTarget(good, "private//var"), after: good},
		{name: "trailing separator", goos: "darwin", component: "var", before: aliasSnapshotWithTarget(good, "private/var/"), after: good},
		{name: "target changed after", goos: "darwin", component: "var", before: good, after: aliasSnapshotWithTarget(good, "private/tmp")},
		{name: "inode changed", goos: "darwin", component: "var", before: good, after: changedInode},
		{name: "device changed", goos: "darwin", component: "var", before: good, after: changedDevice},
		{name: "mode changed", goos: "darwin", component: "var", before: good, after: changedMode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := trustedDarwinVarAlias(test.goos, test.index, test.component, test.before, test.after)
			if got != test.want {
				t.Fatalf("trustedDarwinVarAlias() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestOpenRestrictedPasswordFileRejectsFIFOWithoutBlocking(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "credential.fifo"
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatalf("create credential FIFO: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		file, err := openRestrictedPasswordFile(path)
		if file != nil {
			_ = file.Close()
		}
		result <- err
	}()

	select {
	case err := <-result:
		if err == nil {
			t.Fatal("openRestrictedPasswordFile accepted a FIFO")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("openRestrictedPasswordFile blocked while opening a FIFO")
	}
}

func aliasSnapshotWithTarget(base trustedSystemAliasSnapshot, target string) trustedSystemAliasSnapshot {
	base.target = target
	return base
}
