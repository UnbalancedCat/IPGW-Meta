package candidate

import "os"

func publishSourceMatches(name string, expected os.FileInfo) bool {
	directory, err := openDirectorySnapshot(name)
	if err != nil {
		return false
	}
	defer directory.close()
	return stableFileInfo(expected, directory.info)
}

func publishedIdentityMatches(name string, expected os.FileInfo) bool {
	if expected == nil {
		return false
	}
	directory, err := openDirectorySnapshot(name)
	if err != nil {
		return false
	}
	defer directory.close()
	// A rename may update directory ctime. The object identity and type must
	// nevertheless be the exact verified stage directory.
	return directory.info.IsDir() && directory.info.Mode()&os.ModeSymlink == 0 && os.SameFile(expected, directory.info)
}

func publishSealMatches(validate func(string) bool, root string) bool {
	return validate == nil || validate(root)
}
