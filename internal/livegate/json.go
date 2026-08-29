// Package livegate defines the closed evidence schema for the maintainer-only
// live validation gate.
package livegate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var (
	ErrInvalidEvidenceJSON = errors.New("livegate: invalid evidence JSON")
	ErrEvidenceRead        = errors.New("livegate: could not read evidence JSON")
	ErrEvidenceWrite       = errors.New("livegate: could not write evidence JSON")
)

var evidenceFields = [...]string{
	"schema_version",
	"plan_id",
	"revision",
	"evidence_id",
	"candidate_id",
	"candidate_set_sha256",
	"source_commit",
	"platform",
	"testbed",
	"network_type",
	"auth_method",
	"suite",
	"capability_before",
	"result",
	"capability_after",
	"started_at",
	"finished_at",
	"steps",
}

var stepFields = [...]string{
	"name",
	"result",
	"exit_code",
	"error_code",
	"duration_seconds",
}

type evidenceWire Evidence

func DecodeEvidence(r io.Reader) (Evidence, error) {
	if r == nil {
		return Evidence{}, ErrEvidenceRead
	}

	raw, err := io.ReadAll(io.LimitReader(r, MaxEvidenceJSONBytes+1))
	if err != nil {
		return Evidence{}, ErrEvidenceRead
	}
	return decodeEvidenceBytes(raw)
}

func decodeEvidenceBytes(raw []byte) (Evidence, error) {
	if len(raw) == 0 || len(raw) > MaxEvidenceJSONBytes || !utf8.Valid(raw) {
		return Evidence{}, ErrInvalidEvidenceJSON
	}
	if err := scanSingleJSONValue(raw); err != nil {
		return Evidence{}, ErrInvalidEvidenceJSON
	}
	if err := validateWireFields(raw); err != nil {
		return Evidence{}, ErrInvalidEvidenceJSON
	}

	var wire evidenceWire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return Evidence{}, ErrInvalidEvidenceJSON
	}
	evidence := Evidence(wire)
	if err := evidence.Validate(); err != nil {
		return Evidence{}, err
	}
	return evidence, nil
}

func (e Evidence) MarshalJSON() ([]byte, error) {
	if err := e.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(evidenceWire(e))
}

func (e *Evidence) UnmarshalJSON(raw []byte) error {
	if e == nil {
		return ErrInvalidEvidenceJSON
	}
	decoded, err := decodeEvidenceBytes(raw)
	if err != nil {
		return err
	}
	*e = decoded
	return nil
}

func EncodeEvidence(w io.Writer, evidence Evidence) error {
	if w == nil {
		return ErrEvidenceWrite
	}
	if err := evidence.Validate(); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		return ErrInvalidEvidenceJSON
	}
	raw = append(raw, '\n')
	if len(raw) > MaxEvidenceJSONBytes {
		return ErrInvalidEvidenceJSON
	}
	n, err := w.Write(raw)
	if err != nil || n != len(raw) {
		return ErrEvidenceWrite
	}
	return nil
}

func scanSingleJSONValue(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return ErrInvalidEvidenceJSON
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, depth int) error {
	// Valid evidence has depth three. This protects the duplicate-key scanner
	// without excluding schema-valid input.
	if depth > 64 {
		return ErrInvalidEvidenceJSON
	}

	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidEvidenceJSON
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrInvalidEvidenceJSON
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrInvalidEvidenceJSON
			}
			if _, exists := seen[key]; exists {
				return ErrInvalidEvidenceJSON
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalidEvidenceJSON
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder, depth+1); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalidEvidenceJSON
		}
	default:
		return ErrInvalidEvidenceJSON
	}
	return nil
}

func validateWireFields(raw []byte) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return ErrInvalidEvidenceJSON
	}
	if !hasExactFields(object, evidenceFields[:]) {
		return ErrInvalidEvidenceJSON
	}

	var steps []json.RawMessage
	if err := json.Unmarshal(object["steps"], &steps); err != nil || steps == nil {
		return ErrInvalidEvidenceJSON
	}
	for _, rawStep := range steps {
		var step map[string]json.RawMessage
		if err := json.Unmarshal(rawStep, &step); err != nil || step == nil {
			return ErrInvalidEvidenceJSON
		}
		if !hasExactFields(step, stepFields[:]) ||
			isJSONNull(step["exit_code"]) ||
			isJSONNull(step["duration_seconds"]) {
			return ErrInvalidEvidenceJSON
		}
	}
	return nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func hasExactFields(object map[string]json.RawMessage, required []string) bool {
	if len(object) != len(required) {
		return false
	}
	for _, name := range required {
		if _, exists := object[name]; !exists {
			return false
		}
	}
	return true
}
