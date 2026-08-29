package livegate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
)

var (
	ErrCandidateSecurity = errors.New("livegate: candidate security check failed")
	ErrCandidateDrift    = errors.New("livegate: candidate changed after verification")
)

// VerifiedCandidate binds the exact private candidate and manifest paths to
// the validated manifest projection captured before execution.
type VerifiedCandidate struct {
	Binding       CandidateBinding
	CandidatePath string
	ManifestPath  string

	candidateSnapshot privateFileSnapshot
	manifestSnapshot  privateFileSnapshot
}

// VerifyCandidateBefore securely opens and binds a candidate and its manifest.
// All failures use a fixed sentinel and never include caller-controlled values.
func VerifyCandidateBefore(candidatePath, manifestPath, explicitSHA string, platform Platform) (VerifiedCandidate, error) {
	if !validCandidatePath(candidatePath) ||
		!validCandidatePath(manifestPath) ||
		candidatePathsEqual(candidatePath, manifestPath) ||
		!lowerHex64Pattern.MatchString(explicitSHA) ||
		!platform.valid() {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}

	manifest, manifestSnapshot, err := openPrivateRegularFile(manifestPath)
	if err != nil {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}
	defer manifest.Close()

	binding, err := DecodeCandidateManifest(manifest, platform)
	if err != nil {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}
	if !snapshotStillNamesOpenFile(manifest, manifestSnapshot) {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}

	candidate, candidateSnapshot, err := openPrivateExecutableFile(candidatePath)
	if err != nil {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}
	defer candidate.Close()

	if os.SameFile(candidateSnapshot.info, manifestSnapshot.info) ||
		filepath.Base(candidatePath) != binding.Name ||
		candidateSnapshot.info.Size() != binding.Size ||
		binding.SHA256 != explicitSHA {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}
	actualSHA, err := hashOpenCandidate(candidate, candidateSnapshot.info.Size())
	if err != nil || actualSHA != binding.SHA256 ||
		!snapshotStillNamesOpenFile(candidate, candidateSnapshot) {
		return VerifiedCandidate{}, ErrCandidateSecurity
	}

	return VerifiedCandidate{
		Binding:           binding,
		CandidatePath:     candidatePath,
		ManifestPath:      manifestPath,
		candidateSnapshot: candidateSnapshot,
		manifestSnapshot:  manifestSnapshot,
	}, nil
}

// VerifyAfter securely reopens both files and proves that their identity,
// private metadata, manifest projection, size, and hashes are unchanged.
func (verified VerifiedCandidate) VerifyAfter() error {
	if verified.candidateSnapshot.info == nil ||
		verified.manifestSnapshot.info == nil ||
		!validCandidatePath(verified.CandidatePath) ||
		!validCandidatePath(verified.ManifestPath) ||
		candidatePathsEqual(verified.CandidatePath, verified.ManifestPath) ||
		!verified.Binding.Platform.valid() {
		return ErrCandidateDrift
	}

	manifest, manifestSnapshot, err := openPrivateRegularFile(verified.ManifestPath)
	if err != nil {
		return ErrCandidateDrift
	}
	defer manifest.Close()

	binding, err := DecodeCandidateManifest(manifest, verified.Binding.Platform)
	if err != nil ||
		binding != verified.Binding ||
		!samePrivateFileSnapshot(manifestSnapshot, verified.manifestSnapshot) ||
		!snapshotStillNamesOpenFile(manifest, manifestSnapshot) {
		return ErrCandidateDrift
	}

	candidate, candidateSnapshot, err := openPrivateExecutableFile(verified.CandidatePath)
	if err != nil {
		return ErrCandidateDrift
	}
	defer candidate.Close()

	if os.SameFile(candidateSnapshot.info, manifestSnapshot.info) ||
		filepath.Base(verified.CandidatePath) != binding.Name ||
		!samePrivateFileSnapshot(candidateSnapshot, verified.candidateSnapshot) ||
		candidateSnapshot.info.Size() != binding.Size {
		return ErrCandidateDrift
	}
	actualSHA, err := hashOpenCandidate(candidate, candidateSnapshot.info.Size())
	if err != nil || actualSHA != binding.SHA256 ||
		!snapshotStillNamesOpenFile(candidate, candidateSnapshot) {
		return ErrCandidateDrift
	}
	return nil
}

type privateFileSnapshot struct {
	info                os.FileInfo
	securityFingerprint [sha256.Size]byte
}

func validCandidatePath(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path
}

func samePrivateFileSnapshot(left, right privateFileSnapshot) bool {
	return left.info != nil &&
		right.info != nil &&
		os.SameFile(left.info, right.info) &&
		left.info.Size() == right.info.Size() &&
		left.securityFingerprint == right.securityFingerprint
}

func snapshotStillNamesOpenFile(file *os.File, snapshot privateFileSnapshot) bool {
	info, err := file.Stat()
	return err == nil &&
		snapshot.info != nil &&
		os.SameFile(info, snapshot.info) &&
		info.Size() == snapshot.info.Size()
}

func hashOpenCandidate(file *os.File, expectedSize int64) (string, error) {
	if expectedSize < 1 || expectedSize > MaxCandidateTargetBytes {
		return "", ErrCandidateSecurity
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", ErrCandidateSecurity
	}
	hash := sha256.New()
	written, err := io.Copy(hash, io.LimitReader(file, MaxCandidateTargetBytes+1))
	if err != nil || written != expectedSize {
		return "", ErrCandidateSecurity
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
