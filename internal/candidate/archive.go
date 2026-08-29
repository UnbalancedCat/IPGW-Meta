package candidate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
)

const (
	bundleLicenseName   = "LICENSE"
	bundleLauncherName  = "launcher-default.yaml"
	bundleManifestName  = "bundle-manifest.json"
	bundleChecksumsName = "SHA256SUMS"
)

type bundleFile struct {
	name string
	mode int64
	data []byte
}

type bundleSummary struct {
	members map[string]fileMetadata
}

func createBundle(destination, target string, binaries map[string][]byte, license []byte, epoch int64) (bundleSummary, error) {
	entries := make([]string, 0, len(productOrder))
	files := make(map[string]bundleFile, 7)
	for _, product := range productOrder {
		name := product
		if strings.HasPrefix(target, "windows-") {
			name += ".exe"
		}
		content, ok := binaries[name]
		if !ok || len(content) == 0 || int64(len(content)) > MaxBinaryBytes {
			return bundleSummary{}, ErrAssemble
		}
		entries = append(entries, name)
		files[name] = bundleFile{name: name, mode: 0o755, data: content}
	}
	launcher := []byte("schema_version: 1\nmode: meta\ncohort: new-install\n")
	files[bundleLicenseName] = bundleFile{name: bundleLicenseName, mode: 0o644, data: license}
	files[bundleLauncherName] = bundleFile{name: bundleLauncherName, mode: 0o644, data: launcher}
	manifest := bundleManifest(target, entries, files)
	files[bundleManifestName] = bundleFile{name: bundleManifestName, mode: 0o644, data: manifest}
	checksummed := make(map[string]fileMetadata, 6)
	for name, file := range files {
		checksummed[name] = metadataBytes(file.data)
	}
	checksums, err := checksumBytes(checksummed)
	if err != nil {
		return bundleSummary{}, ErrAssemble
	}
	files[bundleChecksumsName] = bundleFile{name: bundleChecksumsName, mode: 0o644, data: checksums}

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return bundleSummary{}, ErrAssemble
	}
	if strings.HasPrefix(target, "windows-") {
		err = writeZIP(output, files, epoch)
	} else {
		err = writeTarGzip(output, files, epoch)
	}
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(destination)
		return bundleSummary{}, ErrAssemble
	}
	metadata, err := metadataFile(destination, MaxArchiveBytes)
	if err != nil {
		_ = os.Remove(destination)
		return bundleSummary{}, ErrAssemble
	}
	_ = metadata
	return bundleSummary{members: checksummed}, nil
}

func bundleManifest(target string, entries []string, files map[string]bundleFile) []byte {
	metrics := make([]fileMetadata, len(entries))
	for index, name := range entries {
		metrics[index] = metadataBytes(files[name].data)
	}
	return []byte(fmt.Sprintf("{\n"+
		"  \"schema_version\": 1,\n"+
		"  \"product\": \"ipgw-meta\",\n"+
		"  \"module\": \"github.com/UnbalancedCat/ipgw-meta\",\n"+
		"  \"version\": \"v1.0.0\",\n"+
		"  \"platform\": \"%s\",\n"+
		"  \"entries\": [\n"+
		"    {\"path\": \"%s\", \"sha256\": \"%s\", \"size\": %d},\n"+
		"    {\"path\": \"%s\", \"sha256\": \"%s\", \"size\": %d},\n"+
		"    {\"path\": \"%s\", \"sha256\": \"%s\", \"size\": %d}\n"+
		"  ],\n"+
		"  \"launcher_default\": \"meta\",\n"+
		"  \"layout\": \"versioned-bundle-v1\",\n"+
		"  \"self_update\": false,\n"+
		"  \"uninstall\": {\"remove_all_three_entries\": true, \"preserve_user_config\": true}\n"+
		"}\n",
		target,
		entries[0], metrics[0].sha256, metrics[0].size,
		entries[1], metrics[1].sha256, metrics[1].size,
		entries[2], metrics[2].sha256, metrics[2].size,
	))
}

func writeTarGzip(output io.Writer, files map[string]bundleFile, epoch int64) error {
	gzipWriter, err := gzip.NewWriterLevel(output, gzip.BestCompression)
	if err != nil {
		return ErrAssemble
	}
	gzipWriter.Header = gzip.Header{ModTime: time.Unix(epoch, 0).UTC(), OS: 255}
	tarWriter := tar.NewWriter(gzipWriter)
	for _, name := range sortedBundleNames(files) {
		file := files[name]
		header := &tar.Header{
			Name: file.name, Mode: file.mode, Size: int64(len(file.data)),
			ModTime: time.Unix(epoch, 0).UTC(), Typeflag: tar.TypeReg,
			Uid: 0, Gid: 0, Uname: "", Gname: "", Format: tar.FormatUSTAR,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return ErrAssemble
		}
		if _, err := tarWriter.Write(file.data); err != nil {
			_ = tarWriter.Close()
			_ = gzipWriter.Close()
			return ErrAssemble
		}
	}
	if err := tarWriter.Close(); err != nil {
		_ = gzipWriter.Close()
		return ErrAssemble
	}
	if err := gzipWriter.Close(); err != nil {
		return ErrAssemble
	}
	return nil
}

func writeZIP(output io.Writer, files map[string]bundleFile, epoch int64) error {
	zipWriter := zip.NewWriter(output)
	date, clock, ok := zipDOSTime(time.Unix(epoch, 0).UTC())
	if !ok {
		_ = zipWriter.Close()
		return ErrAssemble
	}
	for _, name := range sortedBundleNames(files) {
		file := files[name]
		header := &zip.FileHeader{Name: file.name, Method: zip.Deflate, ModifiedDate: date, ModifiedTime: clock}
		header.SetMode(os.FileMode(file.mode))
		header.Extra = nil
		header.Comment = ""
		member, err := zipWriter.CreateHeader(header)
		if err != nil {
			_ = zipWriter.Close()
			return ErrAssemble
		}
		if _, err := member.Write(file.data); err != nil {
			_ = zipWriter.Close()
			return ErrAssemble
		}
	}
	if err := zipWriter.Close(); err != nil {
		return ErrAssemble
	}
	return nil
}

func sortedBundleNames(files map[string]bundleFile) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func zipDOSTime(value time.Time) (uint16, uint16, bool) {
	value = value.UTC()
	year, month, day := value.Date()
	if year < 1980 || year > 2107 {
		return 0, 0, false
	}
	date := uint16(year-1980)<<9 | uint16(month)<<5 | uint16(day)
	clock := uint16(value.Hour())<<11 | uint16(value.Minute())<<5 | uint16(value.Second()/2)
	return date, clock, true
}

func verifyBundle(raw []byte, target string, epoch int64, validateToolchain bool) (bundleSummary, error) {
	if len(raw) == 0 || int64(len(raw)) > MaxArchiveBytes {
		return bundleSummary{}, ErrVerify
	}
	if strings.HasPrefix(target, "windows-") {
		return verifyZIPBundle(raw, target, epoch, validateToolchain)
	}
	return verifyTarBundle(raw, target, epoch, validateToolchain)
}

func verifyTarBundle(raw []byte, target string, epoch int64, validateToolchain bool) (bundleSummary, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil || gzipReader.Name != "" || gzipReader.Comment != "" || len(gzipReader.Extra) != 0 ||
		!gzipReader.ModTime.Equal(time.Unix(epoch, 0).UTC()) || gzipReader.OS != 255 {
		return bundleSummary{}, ErrVerify
	}
	tarReader := tar.NewReader(gzipReader)
	contents := make(map[string][]byte, 7)
	var previous string
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil || header.Typeflag != tar.TypeReg || header.Format != tar.FormatUSTAR ||
			header.Name <= previous || header.Uid != 0 || header.Gid != 0 ||
			header.Uname != "" || header.Gname != "" ||
			!header.ModTime.Equal(time.Unix(epoch, 0).UTC()) || !validBundleMember(header.Name, header.Mode, header.Size, target) {
			return bundleSummary{}, ErrVerify
		}
		content, err := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
		if err != nil || int64(len(content)) != header.Size {
			return bundleSummary{}, ErrVerify
		}
		contents[header.Name] = content
		previous = header.Name
	}
	if err := gzipReader.Close(); err != nil {
		return bundleSummary{}, ErrVerify
	}
	return verifyCanonicalBundle(raw, contents, target, epoch, validateToolchain)
}

func verifyZIPBundle(raw []byte, target string, epoch int64, validateToolchain bool) (bundleSummary, error) {
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return bundleSummary{}, ErrVerify
	}
	date, clock, ok := zipDOSTime(time.Unix(epoch, 0).UTC())
	if !ok || len(reader.File) != 7 {
		return bundleSummary{}, ErrVerify
	}
	contents := make(map[string][]byte, 7)
	var previous string
	for _, file := range reader.File {
		if file.Name <= previous || file.Method != zip.Deflate || len(file.Extra) != 0 || file.Comment != "" ||
			file.ModifiedDate != date || file.ModifiedTime != clock ||
			file.UncompressedSize64 > uint64(MaxBinaryBytes) ||
			!validBundleMember(file.Name, int64(file.Mode().Perm()), int64(file.UncompressedSize64), target) {
			return bundleSummary{}, ErrVerify
		}
		member, err := file.Open()
		if err != nil {
			return bundleSummary{}, ErrVerify
		}
		content, readErr := io.ReadAll(io.LimitReader(member, int64(file.UncompressedSize64)+1))
		closeErr := member.Close()
		if readErr != nil || closeErr != nil || uint64(len(content)) != file.UncompressedSize64 {
			return bundleSummary{}, ErrVerify
		}
		contents[file.Name] = content
		previous = file.Name
	}
	return verifyCanonicalBundle(raw, contents, target, epoch, validateToolchain)
}

func validBundleMember(name string, mode, size int64, target string) bool {
	windows := strings.HasPrefix(target, "windows-")
	binaryNames := map[string]bool{"ipgw": true, "ipgw-meta": true, "ipgw-legacy": true}
	if windows {
		binaryNames = map[string]bool{"ipgw.exe": true, "ipgw-meta.exe": true, "ipgw-legacy.exe": true}
	}
	if binaryNames[name] {
		return mode == 0o755 && size >= 1 && size <= MaxBinaryBytes
	}
	if name == bundleLicenseName {
		return mode == 0o644 && size >= 1 && size <= 4*1024*1024
	}
	if name == bundleLauncherName || name == bundleManifestName || name == bundleChecksumsName {
		return mode == 0o644 && size >= 1 && size <= MaxManifestBytes
	}
	return false
}

func verifyBundleContents(contents map[string][]byte, target string) (bundleSummary, error) {
	if len(contents) != 7 {
		return bundleSummary{}, ErrVerify
	}
	entries := []string{"ipgw", "ipgw-meta", "ipgw-legacy"}
	if strings.HasPrefix(target, "windows-") {
		entries = []string{"ipgw.exe", "ipgw-meta.exe", "ipgw-legacy.exe"}
	}
	files := make(map[string]bundleFile, 6)
	for _, name := range append(append([]string{}, entries...), bundleLicenseName, bundleLauncherName) {
		content, ok := contents[name]
		if !ok {
			return bundleSummary{}, ErrVerify
		}
		files[name] = bundleFile{name: name, data: content}
	}
	expectedManifest := bundleManifest(target, entries, files)
	if !bytes.Equal(contents[bundleManifestName], expectedManifest) {
		return bundleSummary{}, ErrVerify
	}
	checksummed := make(map[string]fileMetadata, 6)
	for name, content := range contents {
		if name != bundleChecksumsName {
			checksummed[name] = metadataBytes(content)
		}
	}
	expectedChecksums, err := checksumBytes(checksummed)
	if err != nil || !bytes.Equal(contents[bundleChecksumsName], expectedChecksums) {
		return bundleSummary{}, ErrVerify
	}
	return bundleSummary{members: checksummed}, nil
}

func verifyCanonicalBundle(raw []byte, contents map[string][]byte, target string, epoch int64, validateToolchain bool) (bundleSummary, error) {
	summary, err := verifyBundleContents(contents, target)
	if err != nil {
		return bundleSummary{}, ErrVerify
	}
	files := make(map[string]bundleFile, len(contents))
	if validateToolchain {
		for _, product := range productOrder {
			name := product
			if strings.HasPrefix(target, "windows-") {
				name += ".exe"
			}
			if err := validateGoBinary(contents[name], target, product, false, GoVersion); err != nil {
				return bundleSummary{}, ErrVerify
			}
		}
	}
	for name, content := range contents {
		mode := int64(0o644)
		if name == "ipgw" || name == "ipgw-meta" || name == "ipgw-legacy" ||
			name == "ipgw.exe" || name == "ipgw-meta.exe" || name == "ipgw-legacy.exe" {
			mode = 0o755
		}
		files[name] = bundleFile{name: name, mode: mode, data: content}
	}
	var canonical bytes.Buffer
	if strings.HasPrefix(target, "windows-") {
		err = writeZIP(&canonical, files, epoch)
	} else {
		err = writeTarGzip(&canonical, files, epoch)
	}
	if err != nil || !bytes.Equal(raw, canonical.Bytes()) {
		return bundleSummary{}, ErrVerify
	}
	return summary, nil
}
