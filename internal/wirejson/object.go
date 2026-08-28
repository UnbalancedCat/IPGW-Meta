// Package wirejson decodes the small JSON and JSONP envelopes used by the
// internal protocol adapters. It deliberately contains no CAS or Srun
// business semantics.
package wirejson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// ErrInvalid reports an invalid JSON or JSONP object. The error is purposely
// content-free so an untrusted response body cannot be copied into logs or a
// public error.
var ErrInvalid = errors.New("invalid JSON/JSONP object")

// Object is a strictly decoded JSON object. Its backing map and values are
// private so callers cannot accidentally mutate shared parser state.
type Object struct {
	fields map[string]json.RawMessage
}

// DecodeObjectOrJSONP accepts exactly one JSON object or one safe JSONP call
// whose sole argument is a JSON object. Duplicate object keys are rejected at
// every nesting depth, including keys that become equal after JSON unescaping.
func DecodeObjectOrJSONP(data []byte) (Object, error) {
	payload, ok := objectPayload(data)
	if !ok || validateObject(payload) != nil {
		return Object{}, ErrInvalid
	}

	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return Object{}, ErrInvalid
	}
	fields := make(map[string]json.RawMessage, len(decoded))
	for name, raw := range decoded {
		fields[name] = append(json.RawMessage(nil), raw...)
	}
	return Object{fields: fields}, nil
}

// Raw returns a copy of a field's encoded JSON value.
func (o Object) Raw(name string) (json.RawMessage, bool) {
	raw, ok := o.fields[name]
	if !ok {
		return nil, false
	}
	return append(json.RawMessage(nil), raw...), true
}

// Names returns the object's field names in deterministic lexical order.
func (o Object) Names() []string {
	names := make([]string, 0, len(o.fields))
	for name := range o.fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func objectPayload(data []byte) ([]byte, bool) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, false
	}

	// A bare JSON object does not permit a JavaScript semicolon.
	if trimmed[0] == '{' {
		return trimmed, true
	}

	// JSONP permits one optional trailing semicolon, but never two.
	if trimmed[len(trimmed)-1] == ';' {
		trimmed = bytes.TrimSpace(trimmed[:len(trimmed)-1])
		if len(trimmed) == 0 || trimmed[len(trimmed)-1] == ';' {
			return nil, false
		}
	}

	open := bytes.IndexByte(trimmed, '(')
	close := bytes.LastIndexByte(trimmed, ')')
	if open <= 0 || close != len(trimmed)-1 || close <= open {
		return nil, false
	}
	callback := bytes.TrimSpace(trimmed[:open])
	if !validCallback(callback) {
		return nil, false
	}
	payload := bytes.TrimSpace(trimmed[open+1 : close])
	if len(payload) == 0 || payload[0] != '{' {
		return nil, false
	}
	return payload, true
}

func validCallback(callback []byte) bool {
	if len(callback) == 0 || !callbackInitial(callback[0]) {
		return false
	}
	for _, char := range callback[1:] {
		if !callbackInitial(char) && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func callbackInitial(char byte) bool {
	return char == '_' || char == '$' || char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z'
}

func validateObject(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := validateValue(decoder, true); err != nil {
		return ErrInvalid
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func validateValue(decoder *json.Decoder, requireObject bool) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalid
	}
	delimiter, isDelimiter := token.(json.Delim)
	if requireObject && (!isDelimiter || delimiter != '{') {
		return ErrInvalid
	}
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, nameErr := decoder.Token()
			if nameErr != nil {
				return ErrInvalid
			}
			name, ok := nameToken.(string)
			if !ok {
				return ErrInvalid
			}
			if _, duplicate := seen[name]; duplicate {
				return ErrInvalid
			}
			seen[name] = struct{}{}
			if err := validateValue(decoder, false); err != nil {
				return ErrInvalid
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim('}') {
			return ErrInvalid
		}
		return nil

	case '[':
		for decoder.More() {
			if err := validateValue(decoder, false); err != nil {
				return ErrInvalid
			}
		}
		closing, closeErr := decoder.Token()
		if closeErr != nil || closing != json.Delim(']') {
			return ErrInvalid
		}
		return nil

	default:
		return ErrInvalid
	}
}
