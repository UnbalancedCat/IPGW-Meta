package candidate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"github.com/UnbalancedCat/ipgw-meta/internal/livegate"
)

func EncodeManifest(manifest Manifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, ErrInvalidManifest
	}
	return marshalCanonical(manifest)
}

func DecodeManifest(raw []byte) (Manifest, error) {
	var manifest Manifest
	if err := decodeCanonical(raw, &manifest); err != nil || manifest.Validate() != nil {
		return Manifest{}, ErrInvalidManifest
	}
	canonical, err := marshalCanonical(manifest)
	linux, linuxErr := livegate.DecodeCandidateManifest(bytes.NewReader(raw), livegate.PlatformLinuxAMD64)
	windows, windowsErr := livegate.DecodeCandidateManifest(bytes.NewReader(raw), livegate.PlatformWindowsAMD64)
	if linuxErr != nil || windowsErr != nil ||
		linux.CandidateID != manifest.CandidateID || windows.CandidateID != manifest.CandidateID ||
		linux.SourceCommit != manifest.SourceCommit || windows.SourceCommit != manifest.SourceCommit ||
		linux.Name != manifest.LiveGateTargets[0].Name || linux.Size != manifest.LiveGateTargets[0].Size || linux.SHA256 != manifest.LiveGateTargets[0].SHA256 ||
		windows.Name != manifest.LiveGateTargets[1].Name || windows.Size != manifest.LiveGateTargets[1].Size || windows.SHA256 != manifest.LiveGateTargets[1].SHA256 {
		return Manifest{}, ErrInvalidManifest
	}
	if err != nil || !bytes.Equal(raw, canonical) {
		return Manifest{}, ErrInvalidManifest
	}
	return manifest, nil
}

func EncodeReleaseManifest(manifest ReleaseManifest) ([]byte, error) {
	if err := manifest.Validate(); err != nil {
		return nil, ErrInvalidManifest
	}
	return marshalCanonical(manifest)
}

func DecodeReleaseManifest(raw []byte) (ReleaseManifest, error) {
	var manifest ReleaseManifest
	if err := decodeCanonical(raw, &manifest); err != nil || manifest.Validate() != nil {
		return ReleaseManifest{}, ErrInvalidManifest
	}
	canonical, err := marshalCanonical(manifest)
	if err != nil || !bytes.Equal(raw, canonical) {
		return ReleaseManifest{}, ErrInvalidManifest
	}
	return manifest, nil
}

func marshalCanonical(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, ErrInvalidManifest
	}
	raw = append(raw, '\n')
	if int64(len(raw)) > MaxManifestBytes {
		return nil, ErrInvalidManifest
	}
	return raw, nil
}

func decodeCanonical(raw []byte, target any) error {
	if len(raw) == 0 || int64(len(raw)) > MaxManifestBytes || !utf8.Valid(raw) || validateJSON(raw) != nil {
		return ErrInvalidManifest
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return ErrInvalidManifest
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidManifest
	}
	return nil
}

func validateJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := validateJSONValue(decoder, true, 0); err != nil {
		return ErrInvalidManifest
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidManifest
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder, requireObject bool, depth int) error {
	if depth > 64 {
		return ErrInvalidManifest
	}
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidManifest
	}
	delim, container := token.(json.Delim)
	if requireObject && (!container || delim != '{') {
		return ErrInvalidManifest
	}
	if !container {
		return nil
	}
	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidManifest
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalidManifest
			}
			if _, duplicate := seen[key]; duplicate {
				return ErrInvalidManifest
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder, false, depth+1); err != nil {
				return ErrInvalidManifest
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return ErrInvalidManifest
		}
		return nil
	case '[':
		for decoder.More() {
			if err := validateJSONValue(decoder, false, depth+1); err != nil {
				return ErrInvalidManifest
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return ErrInvalidManifest
		}
		return nil
	default:
		return ErrInvalidManifest
	}
}
