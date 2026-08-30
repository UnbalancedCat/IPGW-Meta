package candidate

import (
	"os"
	"strings"
)

// candidateSeal owns the directory and regular-file handles for one closed
// candidate snapshot. Keeping it open prevents path re-resolution between
// verification and publication.
type candidateSeal struct {
	root               string
	rootDirectory      *directorySnapshot
	releaseDirectory   *directorySnapshot
	testToolsDirectory *directorySnapshot
	rootEntryNames     []string
	releaseNames       []string
	toolNames          []string
	paths              []string
	snapshots          map[string]*regularSnapshot
	metadata           map[string]fileMetadata
}

func openCandidateSeal(root string) (*candidateSeal, error) {
	if !absoluteClean(root) {
		return nil, ErrVerify
	}
	seal := &candidateSeal{
		root:           root,
		rootEntryNames: []string{"SHA256SUMS", "candidate-manifest.json", "release", "test-tools"},
		releaseNames:   make([]string, 0, 10),
		toolNames:      make([]string, 0, 2),
		paths:          []string{"SHA256SUMS", "candidate-manifest.json"},
		snapshots:      make(map[string]*regularSnapshot, 14),
		metadata:       make(map[string]fileMetadata, 14),
	}
	valid := false
	defer func() {
		if !valid {
			seal.close()
		}
	}()
	for _, item := range expectedReleaseAssets() {
		seal.releaseNames = append(seal.releaseNames, strings.TrimPrefix(item.name, "release/"))
		seal.paths = append(seal.paths, item.name)
	}
	for _, item := range expectedTestTools() {
		seal.toolNames = append(seal.toolNames, strings.TrimPrefix(item.name, "test-tools/"))
		seal.paths = append(seal.paths, item.name)
	}
	var err error
	seal.rootDirectory, err = openDirectorySnapshot(root)
	if err != nil || !seal.rootDirectory.exact(seal.rootEntryNames) {
		return nil, ErrVerify
	}
	seal.releaseDirectory, err = openDirectorySnapshotAt(seal.rootDirectory, "release")
	if err != nil {
		return nil, ErrVerify
	}
	seal.testToolsDirectory, err = openDirectorySnapshotAt(seal.rootDirectory, "test-tools")
	if err != nil || !seal.releaseDirectory.exact(seal.releaseNames) || !seal.testToolsDirectory.exact(seal.toolNames) {
		return nil, ErrVerify
	}
	infos := make([]os.FileInfo, 0, 14)
	for _, name := range seal.paths {
		directory := seal.rootDirectory
		basename := name
		if strings.HasPrefix(name, "release/") {
			directory = seal.releaseDirectory
			basename = strings.TrimPrefix(name, "release/")
		} else if strings.HasPrefix(name, "test-tools/") {
			directory = seal.testToolsDirectory
			basename = strings.TrimPrefix(name, "test-tools/")
		}
		snapshot, err := openRegularSnapshotAt(directory.file, basename, artifactMaximum(name))
		if err != nil {
			return nil, ErrVerify
		}
		seal.snapshots[name] = snapshot
		seal.metadata[name] = snapshot.metadata
		infos = append(infos, snapshot.info)
	}
	if sameInputFile(infos) || !seal.unchanged(root) {
		return nil, ErrVerify
	}
	valid = true
	return seal, nil
}

func (seal *candidateSeal) unchanged(root string) bool {
	if seal == nil {
		return false
	}
	for _, name := range seal.paths {
		if !seal.snapshots[name].unchanged() {
			return false
		}
	}
	return directoryUnchangedAt(seal.rootDirectory, seal.releaseDirectory, "release", seal.releaseNames) &&
		directoryUnchangedAt(seal.rootDirectory, seal.testToolsDirectory, "test-tools", seal.toolNames) &&
		directoryUnchanged(root, seal.rootDirectory, seal.rootEntryNames)
}

func (seal *candidateSeal) unchangedAfterRename(root string) bool {
	if seal == nil {
		return false
	}
	for _, name := range seal.paths {
		if !seal.snapshots[name].unchanged() {
			return false
		}
	}
	return directoryUnchangedAt(seal.rootDirectory, seal.releaseDirectory, "release", seal.releaseNames) &&
		directoryUnchangedAt(seal.rootDirectory, seal.testToolsDirectory, "test-tools", seal.toolNames) &&
		directoryRenamedUnchanged(root, seal.rootDirectory, seal.rootEntryNames)
}

func (seal *candidateSeal) close() {
	if seal == nil {
		return
	}
	for _, snapshot := range seal.snapshots {
		snapshot.close()
	}
	seal.testToolsDirectory.close()
	seal.releaseDirectory.close()
	seal.rootDirectory.close()
}
