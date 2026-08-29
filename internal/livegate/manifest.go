package livegate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	CandidateManifestSchemaVersion = 1
	MaxCandidateManifestJSONBytes  = 64 * 1024
	MaxCandidateTargetBytes        = 64 * 1024 * 1024
	candidateVersion               = "v1.0.0"
)

var (
	ErrInvalidCandidateManifest = errors.New("livegate: invalid candidate manifest")
	ErrCandidateManifestRead    = errors.New("livegate: could not read candidate manifest")
)

// CandidateBinding is the validated runner projection of a candidate manifest.
// CandidateSetSHA256 is the SHA-256 of the exact manifest bytes accepted by the
// decoder, including any permitted trailing JSON whitespace.
type CandidateBinding struct {
	CandidateID        string
	CandidateSetSHA256 string
	SourceCommit       string
	Platform           Platform
	Name               string
	Size               int64
	SHA256             string
}

var candidateManifestRequiredFields = [...]string{
	"schema_version",
	"plan_id",
	"revision",
	"version",
	"candidate_id",
	"source_commit",
	"live_gate_targets",
}

var candidateTargetFields = [...]string{
	"platform",
	"name",
	"size",
	"sha256",
}

type candidateTarget struct {
	platform Platform
	name     string
	size     int64
	sha256   string
}

type candidateManifestProjection struct {
	schemaVersion int64
	planID        string
	revision      string
	version       string
	candidateID   string
	sourceCommit  string
	targets       [2]candidateTarget
}

type candidateManifestSource struct {
	reader io.Reader
	err    error
}

func (source *candidateManifestSource) Read(buffer []byte) (int, error) {
	n, err := source.reader.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) && source.err == nil {
		source.err = err
	}
	return n, err
}

// DecodeCandidateManifest validates the live-gate projection in one token
// stream. Unknown top-level values are recursively consumed without retention;
// live_gate_targets remains exact and closed.
func DecodeCandidateManifest(r io.Reader, platform Platform) (CandidateBinding, error) {
	if r == nil {
		return CandidateBinding{}, ErrCandidateManifestRead
	}

	source := &candidateManifestSource{reader: r}
	limited := &io.LimitedReader{
		R: source,
		N: MaxCandidateManifestJSONBytes + 1,
	}
	digest := sha256.New()
	decoder := json.NewDecoder(newCandidateUTF8Reader(io.TeeReader(limited, digest)))
	decoder.UseNumber()

	projection, valid := decodeCandidateManifestObject(decoder)
	if valid {
		_, trailingErr := decoder.Token()
		valid = trailingErr == io.EOF
	}
	if source.err != nil {
		return CandidateBinding{}, ErrCandidateManifestRead
	}
	if limited.N == 0 || !valid || !platform.valid() {
		return CandidateBinding{}, ErrInvalidCandidateManifest
	}
	if projection.schemaVersion != CandidateManifestSchemaVersion ||
		projection.planID != PlanID ||
		projection.revision != Revision ||
		projection.version != candidateVersion ||
		!lowerHex40Pattern.MatchString(projection.sourceCommit) ||
		validateCandidateID(projection.candidateID, projection.sourceCommit) != nil {
		return CandidateBinding{}, ErrInvalidCandidateManifest
	}

	selected := projection.targets[0]
	if platform == PlatformWindowsAMD64 {
		selected = projection.targets[1]
	}
	return CandidateBinding{
		CandidateID:        projection.candidateID,
		CandidateSetSHA256: hex.EncodeToString(digest.Sum(nil)),
		SourceCommit:       projection.sourceCommit,
		Platform:           selected.platform,
		Name:               selected.name,
		Size:               selected.size,
		SHA256:             selected.sha256,
	}, nil
}

func decodeCandidateManifestObject(
	decoder *json.Decoder,
) (candidateManifestProjection, bool) {
	var projection candidateManifestProjection
	token, ok := candidateManifestToken(decoder)
	if !ok || token != json.Delim('{') {
		return projection, false
	}

	seen := make(map[string]struct{})
	for decoder.More() {
		keyToken, ok := candidateManifestToken(decoder)
		if !ok {
			return candidateManifestProjection{}, false
		}
		key, ok := keyToken.(string)
		if !ok {
			return candidateManifestProjection{}, false
		}
		if _, duplicate := seen[key]; duplicate {
			return candidateManifestProjection{}, false
		}
		seen[key] = struct{}{}

		switch key {
		case "schema_version":
			value, ok := candidateManifestPositiveInteger(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.schemaVersion = value
		case "plan_id":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.planID = value
		case "revision":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.revision = value
		case "version":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.version = value
		case "candidate_id":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.candidateID = value
		case "source_commit":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.sourceCommit = value
		case "live_gate_targets":
			targets, ok := decodeCandidateTargets(decoder)
			if !ok {
				return candidateManifestProjection{}, false
			}
			projection.targets = targets
		default:
			if !skipCandidateManifestValue(decoder, 1) {
				return candidateManifestProjection{}, false
			}
		}
	}
	end, ok := candidateManifestToken(decoder)
	if !ok || end != json.Delim('}') {
		return candidateManifestProjection{}, false
	}
	for _, field := range candidateManifestRequiredFields {
		if _, exists := seen[field]; !exists {
			return candidateManifestProjection{}, false
		}
	}
	return projection, true
}

func decodeCandidateTargets(decoder *json.Decoder) ([2]candidateTarget, bool) {
	var decoded [2]candidateTarget
	token, ok := candidateManifestToken(decoder)
	if !ok || token != json.Delim('[') {
		return decoded, false
	}

	count := 0
	valid := true
	for decoder.More() {
		if count < len(decoded) {
			target, targetOK := decodeCandidateTarget(decoder, count)
			if !targetOK {
				return [2]candidateTarget{}, false
			}
			decoded[count] = target
		} else {
			if !skipCandidateManifestValue(decoder, 2) {
				return [2]candidateTarget{}, false
			}
			valid = false
		}
		count++
	}
	end, ok := candidateManifestToken(decoder)
	if !ok || end != json.Delim(']') || count != len(decoded) || !valid {
		return [2]candidateTarget{}, false
	}
	return decoded, true
}

func decodeCandidateTarget(
	decoder *json.Decoder,
	index int,
) (candidateTarget, bool) {
	var target candidateTarget
	token, ok := candidateManifestToken(decoder)
	if !ok || token != json.Delim('{') {
		return target, false
	}

	seen := make(map[string]struct{})
	valid := true
	for decoder.More() {
		keyToken, ok := candidateManifestToken(decoder)
		if !ok {
			return candidateTarget{}, false
		}
		key, ok := keyToken.(string)
		if !ok {
			return candidateTarget{}, false
		}
		if _, duplicate := seen[key]; duplicate {
			return candidateTarget{}, false
		}
		seen[key] = struct{}{}

		switch key {
		case "platform":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateTarget{}, false
			}
			target.platform = Platform(value)
		case "name":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateTarget{}, false
			}
			target.name = value
		case "size":
			value, ok := candidateManifestPositiveInteger(decoder)
			if !ok {
				return candidateTarget{}, false
			}
			target.size = value
		case "sha256":
			value, ok := candidateManifestString(decoder)
			if !ok {
				return candidateTarget{}, false
			}
			target.sha256 = value
		default:
			if !skipCandidateManifestValue(decoder, 3) {
				return candidateTarget{}, false
			}
			valid = false
		}
	}
	end, ok := candidateManifestToken(decoder)
	if !ok || end != json.Delim('}') {
		return candidateTarget{}, false
	}
	for _, field := range candidateTargetFields {
		if _, exists := seen[field]; !exists {
			valid = false
		}
	}

	expectedPlatforms := [...]Platform{PlatformLinuxAMD64, PlatformWindowsAMD64}
	expectedNames := [...]string{"ipgw-meta", "ipgw-meta.exe"}
	if index < 0 || index >= len(expectedPlatforms) ||
		target.platform != expectedPlatforms[index] ||
		target.name != expectedNames[index] ||
		target.size < 1 ||
		target.size > MaxCandidateTargetBytes ||
		!lowerHex64Pattern.MatchString(target.sha256) {
		valid = false
	}
	return target, valid
}

func skipCandidateManifestValue(decoder *json.Decoder, depth int) bool {
	token, ok := candidateManifestToken(decoder)
	if !ok {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	if depth >= MaxCandidateJSONDepth {
		return false
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, ok := candidateManifestToken(decoder)
			if !ok {
				return false
			}
			key, ok := keyToken.(string)
			if !ok {
				return false
			}
			if _, duplicate := seen[key]; duplicate {
				return false
			}
			seen[key] = struct{}{}
			if !skipCandidateManifestValue(decoder, depth+1) {
				return false
			}
		}
		end, ok := candidateManifestToken(decoder)
		return ok && end == json.Delim('}')
	case '[':
		for decoder.More() {
			if !skipCandidateManifestValue(decoder, depth+1) {
				return false
			}
		}
		end, ok := candidateManifestToken(decoder)
		return ok && end == json.Delim(']')
	default:
		return false
	}
}

func candidateManifestToken(decoder *json.Decoder) (any, bool) {
	token, err := decoder.Token()
	if err != nil {
		return nil, false
	}
	if value, ok := token.(string); ok && strings.ContainsRune(value, utf8.RuneError) {
		return nil, false
	}
	return token, true
}

func candidateManifestString(decoder *json.Decoder) (string, bool) {
	token, ok := candidateManifestToken(decoder)
	if !ok {
		return "", false
	}
	value, ok := token.(string)
	return value, ok
}

func candidateManifestPositiveInteger(decoder *json.Decoder) (int64, bool) {
	token, ok := candidateManifestToken(decoder)
	if !ok {
		return 0, false
	}
	number, ok := token.(json.Number)
	if !ok {
		return 0, false
	}
	text := number.String()
	if text == "" {
		return 0, false
	}
	for _, digit := range text {
		if digit < '0' || digit > '9' {
			return 0, false
		}
	}
	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 1 {
		return 0, false
	}
	return value, true
}
