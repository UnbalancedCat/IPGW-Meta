package livegate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeEvidencePassFixtures(t *testing.T) {
	for _, evidence := range []Evidence{validPasswordEvidence(), validTerminalQREvidence()} {
		t.Run(string(evidence.Suite), func(t *testing.T) {
			raw := mustEncodeEvidence(t, evidence)
			got, err := DecodeEvidence(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("DecodeEvidence() error = %v", err)
			}
			if !reflect.DeepEqual(got, evidence) {
				t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", got, evidence)
			}
		})
	}
}

func TestWireFieldsAreExact(t *testing.T) {
	raw := mustEncodeEvidence(t, validPasswordEvidence())
	object := mustJSONObject(t, raw)
	if len(evidenceFields) != 18 {
		t.Fatalf("schema top-level field list = %d, want 18", len(evidenceFields))
	}
	if len(object) != len(evidenceFields) {
		t.Fatalf("top-level fields = %d, want exactly 18", len(object))
	}
	for _, name := range evidenceFields {
		if _, ok := object[name]; !ok {
			t.Errorf("encoded evidence missing field %q", name)
		}
	}
	steps := mustJSONArrayField(t, object, "steps")
	firstStep, ok := steps[0].(map[string]any)
	if !ok {
		t.Fatalf("first step type = %T, want object", steps[0])
	}
	if len(stepFields) != 5 {
		t.Fatalf("schema step field list = %d, want 5", len(stepFields))
	}
	if len(firstStep) != len(stepFields) {
		t.Fatalf("step fields = %d, want exactly 5", len(firstStep))
	}
	for _, name := range stepFields {
		if _, ok := firstStep[name]; !ok {
			t.Errorf("encoded step missing field %q", name)
		}
	}

	for _, field := range evidenceFields {
		field := field
		t.Run("missing_top_"+field, func(t *testing.T) {
			candidate := mustJSONObject(t, raw)
			delete(candidate, field)
			requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
		})
	}
	for _, field := range stepFields {
		field := field
		t.Run("missing_step_"+field, func(t *testing.T) {
			candidate := mustJSONObject(t, raw)
			candidateSteps := mustJSONArrayField(t, candidate, "steps")
			candidateStep := candidateSteps[0].(map[string]any)
			delete(candidateStep, field)
			requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
		})
	}

	t.Run("unknown_top", func(t *testing.T) {
		candidate := mustJSONObject(t, raw)
		candidate["notes"] = "not allowed"
		requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
	})
	t.Run("unknown_step", func(t *testing.T) {
		candidate := mustJSONObject(t, raw)
		candidateSteps := mustJSONArrayField(t, candidate, "steps")
		candidateSteps[0].(map[string]any)["details"] = "not allowed"
		requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
	})
	t.Run("case_variant_top", func(t *testing.T) {
		candidate := mustJSONObject(t, raw)
		candidate["Plan_ID"] = candidate["plan_id"]
		delete(candidate, "plan_id")
		requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
	})
	t.Run("case_variant_step", func(t *testing.T) {
		candidate := mustJSONObject(t, raw)
		candidateSteps := mustJSONArrayField(t, candidate, "steps")
		step := candidateSteps[0].(map[string]any)
		step["Name"] = step["name"]
		delete(step, "name")
		requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, candidate)))
	})
	t.Run("case_variant_enum", func(t *testing.T) {
		candidate := mustJSONObject(t, raw)
		candidate["suite"] = "Password_Core"
		requireInvalidEvidence(t, decodeBytes(mustMarshalJSON(t, candidate)))
	})
}

func TestDuplicateObjectKeysAreRejectedAtEveryObjectLevel(t *testing.T) {
	raw := mustEncodeEvidence(t, validPasswordEvidence())

	duplicateTop := replaceOnce(t, raw,
		[]byte("{\n  \"schema_version\""),
		[]byte("{\n  \"plan_id\": \"IPGW-META-V1\",\n  \"schema_version\""),
	)
	requireInvalidJSON(t, decodeBytes(duplicateTop))

	duplicateEscapedTop := replaceOnce(t, raw,
		[]byte("{\n  \"schema_version\""),
		[]byte("{\n  \"plan\\u005fid\": \"IPGW-META-V1\",\n  \"schema_version\""),
	)
	requireInvalidJSON(t, decodeBytes(duplicateEscapedTop))

	duplicateStep := replaceOnce(t, raw,
		[]byte("    {\n      \"name\": \"initial_status_offline\","),
		[]byte("    {\n      \"name\": \"initial_status_offline\",\n      \"name\": \"initial_status_offline\","),
	)
	requireInvalidJSON(t, decodeBytes(duplicateStep))

	end := bytes.LastIndex(raw, []byte("\n}"))
	if end < 0 {
		t.Fatal("encoded evidence does not end in an object")
	}
	nestedDuplicate := append([]byte(nil), raw[:end]...)
	nestedDuplicate = append(nestedDuplicate, []byte(",\n  \"unknown_nested\": {\"leaf\": 1, \"leaf\": 2}")...)
	nestedDuplicate = append(nestedDuplicate, raw[end:]...)
	requireInvalidJSON(t, decodeBytes(nestedDuplicate))
}

func TestTrailingJSONWhitespaceUTF8AndSize(t *testing.T) {
	raw := mustEncodeEvidence(t, validPasswordEvidence())

	withWhitespace := append(append([]byte(nil), raw...), []byte(" \t\r\n")...)
	if _, err := DecodeEvidence(bytes.NewReader(withWhitespace)); err != nil {
		t.Fatalf("trailing JSON whitespace should be accepted: %v", err)
	}

	requireInvalidJSON(t, decodeBytes(append(append([]byte(nil), raw...), []byte("{}")...)))
	requireInvalidJSON(t, decodeBytes(append(append([]byte(nil), raw...), 'x')))
	requireInvalidJSON(t, decodeBytes(nil))
	requireInvalidJSON(t, decodeBytes([]byte(" \t\r\n")))
	requireInvalidJSON(t, decodeBytes([]byte("{\"schema_version\":1")))

	invalidUTF8 := append(append([]byte(nil), raw...), byte(0xff))
	requireInvalidJSON(t, decodeBytes(invalidUTF8))

	if len(raw) >= MaxEvidenceJSONBytes {
		t.Fatalf("fixture size = %d, expected below limit", len(raw))
	}
	exactLimit := append(append([]byte(nil), raw...), bytes.Repeat([]byte{' '}, MaxEvidenceJSONBytes-len(raw))...)
	if _, err := DecodeEvidence(bytes.NewReader(exactLimit)); err != nil {
		t.Fatalf("exact 64 KiB evidence should be accepted: %v", err)
	}
	overLimit := append(exactLimit, ' ')
	requireInvalidJSON(t, decodeBytes(overLimit))
}

func TestWireTypesAndIntegerNumericForms(t *testing.T) {
	raw := mustEncodeEvidence(t, validPasswordEvidence())
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{"schema_string", "\"schema_version\": 1", "\"schema_version\": \"1\""},
		{"schema_null", "\"schema_version\": 1", "\"schema_version\": null"},
		{"schema_decimal", "\"schema_version\": 1", "\"schema_version\": 1.0"},
		{"schema_exponent", "\"schema_version\": 1", "\"schema_version\": 1e0"},
		{"exit_string", "\"exit_code\": 0", "\"exit_code\": \"0\""},
		{"exit_null", "\"exit_code\": 0", "\"exit_code\": null"},
		{"exit_decimal", "\"exit_code\": 0", "\"exit_code\": 0.0"},
		{"exit_exponent", "\"exit_code\": 0", "\"exit_code\": 0e0"},
		{"duration_string", "\"duration_seconds\": 1", "\"duration_seconds\": \"1\""},
		{"duration_null", "\"duration_seconds\": 1", "\"duration_seconds\": null"},
		{"duration_decimal", "\"duration_seconds\": 1", "\"duration_seconds\": 1.0"},
		{"duration_exponent", "\"duration_seconds\": 1", "\"duration_seconds\": 1e0"},
		{"duration_fraction", "\"duration_seconds\": 1", "\"duration_seconds\": 1.5"},
		{"duration_negative", "\"duration_seconds\": 1", "\"duration_seconds\": -1"},
		{"error_number", "\"error_code\": null", "\"error_code\": 0"},
		{"error_boolean", "\"error_code\": null", "\"error_code\": false"},
		{"result_number", "\"result\": \"pass\"", "\"result\": 1"},
		{"started_at_number", "\"started_at\": \"2026-08-29T01:02:03Z\"", "\"started_at\": 1"},
		{"capability_before_string", "\"capability_before\": [", "\"capability_before\": \"synthetic_covered\", \"discarded\": ["},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := replaceOnce(t, raw, []byte(test.old), []byte(test.new))
			_, err := DecodeEvidence(bytes.NewReader(candidate))
			if err == nil {
				t.Fatal("DecodeEvidence() accepted wrong wire type or non-integer numeric form")
			}
		})
	}

	nullCapabilities := mustJSONObject(t, raw)
	nullCapabilities["capability_before"] = nil
	requireInvalidEvidence(t, decodeBytes(mustMarshalJSON(t, nullCapabilities)))

	objectSteps := mustJSONObject(t, raw)
	objectSteps["steps"] = map[string]any{}
	requireInvalidJSON(t, decodeBytes(mustMarshalJSON(t, objectSteps)))
}

func TestEncodeEvidenceIsCanonicalAndRoundTrips(t *testing.T) {
	evidence := validPasswordEvidence()
	raw := mustEncodeEvidence(t, evidence)
	if !bytes.HasSuffix(raw, []byte{'\n'}) {
		t.Fatal("canonical encoding does not end in LF")
	}
	if bytes.HasSuffix(raw, []byte("\n\n")) {
		t.Fatal("canonical encoding ends in more than one LF")
	}
	if bytes.Contains(raw, []byte{'\r'}) {
		t.Fatal("canonical encoding contains CR")
	}
	if !bytes.HasPrefix(raw, []byte("{\n  \"schema_version\": 1,")) {
		t.Fatalf("canonical encoding has unexpected prefix: %q", raw[:min(40, len(raw))])
	}

	decoded, err := DecodeEvidence(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("DecodeEvidence() error = %v", err)
	}
	if !reflect.DeepEqual(decoded, evidence) {
		t.Fatalf("round trip mismatch\ngot:  %#v\nwant: %#v", decoded, evidence)
	}
	second := mustEncodeEvidence(t, decoded)
	if !bytes.Equal(second, raw) {
		t.Fatal("canonical encoding is not deterministic after round trip")
	}
}

func TestNilShortWriterAndReadWriteErrors(t *testing.T) {
	evidence := validPasswordEvidence()
	if err := EncodeEvidence(nil, evidence); !errors.Is(err, ErrEvidenceWrite) {
		t.Fatalf("EncodeEvidence(nil) error = %v, want ErrEvidenceWrite", err)
	}
	if _, err := DecodeEvidence(nil); !errors.Is(err, ErrEvidenceRead) {
		t.Fatalf("DecodeEvidence(nil) error = %v, want ErrEvidenceRead", err)
	}

	if err := EncodeEvidence(shortWriter{}, evidence); !errors.Is(err, ErrEvidenceWrite) {
		t.Fatalf("EncodeEvidence(short writer) error = %v, want ErrEvidenceWrite", err)
	}

	const canary = "CANARY_reader_writer_secret_281d"
	underlying := fmt.Errorf("transport %s: %w", canary, io.ErrUnexpectedEOF)
	if err := EncodeEvidence(errorWriter{err: underlying}, evidence); !errors.Is(err, ErrEvidenceWrite) {
		t.Fatalf("EncodeEvidence(error writer) error = %v, want ErrEvidenceWrite", err)
	} else {
		assertErrorTreeDoesNotContain(t, err, canary)
	}
	if _, err := DecodeEvidence(&errorReader{err: underlying}); !errors.Is(err, ErrEvidenceRead) {
		t.Fatalf("DecodeEvidence(error reader) error = %v, want ErrEvidenceRead", err)
	} else {
		assertErrorTreeDoesNotContain(t, err, canary)
	}
}

func TestDecodeAndEncodeErrorsNeverLeakCanaries(t *testing.T) {
	const canary = "CANARY_password_cookie_ticket_73be?secret=yes"
	raw := mustEncodeEvidence(t, validPasswordEvidence())

	object := mustJSONObject(t, raw)
	object["candidate_id"] = canary
	err := decodeBytes(mustMarshalJSON(t, object))
	requireInvalidEvidence(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)

	object = mustJSONObject(t, raw)
	object["notes"] = canary
	err = decodeBytes(mustMarshalJSON(t, object))
	requireInvalidJSON(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)

	err = decodeBytes([]byte("{\"unknown\":\"" + canary + "\""))
	requireInvalidJSON(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)

	evidence := validPasswordEvidence()
	evidence.CandidateID = canary
	err = EncodeEvidence(io.Discard, evidence)
	requireInvalidEvidence(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)
}

func mustEncodeEvidence(t *testing.T, evidence Evidence) []byte {
	t.Helper()
	var buffer bytes.Buffer
	if err := EncodeEvidence(&buffer, evidence); err != nil {
		t.Fatalf("EncodeEvidence() error = %v", err)
	}
	return buffer.Bytes()
}

func mustJSONObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return object
}

func mustJSONArrayField(t *testing.T, object map[string]any, name string) []any {
	t.Helper()
	values, ok := object[name].([]any)
	if !ok {
		t.Fatalf("field %q type = %T, want array", name, object[name])
	}
	if len(values) == 0 {
		t.Fatalf("field %q is empty", name)
	}
	return values
}

func mustMarshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return raw
}

func replaceOnce(t *testing.T, raw, old, replacement []byte) []byte {
	t.Helper()
	if bytes.Count(raw, old) == 0 {
		t.Fatalf("fixture does not contain %q", old)
	}
	return bytes.Replace(raw, old, replacement, 1)
}

func decodeBytes(raw []byte) error {
	_, err := DecodeEvidence(bytes.NewReader(raw))
	return err
}

func requireInvalidJSON(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected invalid evidence JSON error")
	}
	if !errors.Is(err, ErrInvalidEvidenceJSON) {
		t.Fatalf("error = %v, want ErrInvalidEvidenceJSON", err)
	}
}

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) - 1, nil
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

type errorReader struct {
	err  error
	sent bool
}

func (r *errorReader) Read(p []byte) (int, error) {
	if !r.sent && len(p) > 0 {
		r.sent = true
		p[0] = '{'
		return 1, nil
	}
	return 0, r.err
}

func TestErrorTreeHelperTraversesSingleAndMultiUnwrap(t *testing.T) {
	const absent = "CANARY_absent_from_error_tree"
	err := errors.Join(
		fmt.Errorf("level one: %w", ErrInvalidEvidence),
		fmt.Errorf("level two: %w", ErrInvalidEvidenceJSON),
	)
	assertErrorTreeDoesNotContain(t, err, absent)
	if strings.Contains(err.Error(), absent) {
		t.Fatal("test precondition failed")
	}
}
func TestDirectJSONMethodsRemainStrict(t *testing.T) {
	const canary = "CANARY_direct_json_secret_8d6a"
	evidence := validPasswordEvidence()
	raw := mustEncodeEvidence(t, evidence)

	compact, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("json.Marshal(valid Evidence) error = %v", err)
	}
	var decoded Evidence
	if err := json.Unmarshal(compact, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(valid Evidence) error = %v", err)
	}
	if !reflect.DeepEqual(decoded, evidence) {
		t.Fatalf("direct JSON round trip mismatch\ngot:  %#v\nwant: %#v", decoded, evidence)
	}

	invalid := cloneEvidence(evidence)
	invalid.Suite = Suite(canary)
	if _, err := json.Marshal(invalid); err == nil {
		t.Fatal("json.Marshal(invalid Evidence) succeeded")
	} else {
		if !errors.Is(err, ErrInvalidEvidence) {
			t.Fatalf("json.Marshal(invalid Evidence) error = %v, want ErrInvalidEvidence", err)
		}
		assertErrorTreeDoesNotContain(t, err, canary)
	}

	unknown := replaceOnce(t, raw,
		[]byte("{\n  \"schema_version\""),
		[]byte("{\n  \"notes\": \""+canary+"\",\n  \"schema_version\""),
	)
	if err := json.Unmarshal(unknown, &decoded); err == nil {
		t.Fatal("json.Unmarshal(unknown field) succeeded")
	} else {
		requireInvalidJSON(t, err)
		assertErrorTreeDoesNotContain(t, err, canary)
	}

	duplicate := replaceOnce(t, raw,
		[]byte("{\n  \"schema_version\""),
		[]byte("{\n  \"plan_id\": \"IPGW-META-V1\",\n  \"schema_version\""),
	)
	if err := json.Unmarshal(duplicate, &decoded); err == nil {
		t.Fatal("json.Unmarshal(duplicate field) succeeded")
	} else {
		requireInvalidJSON(t, err)
	}

	missing := mustJSONObject(t, raw)
	delete(missing, "revision")
	if err := json.Unmarshal(mustMarshalJSON(t, missing), &decoded); err == nil {
		t.Fatal("json.Unmarshal(missing field) succeeded")
	} else {
		requireInvalidJSON(t, err)
	}

	nullExit := replaceOnce(t, raw, []byte("\"exit_code\": 0"), []byte("\"exit_code\": null"))
	if err := json.Unmarshal(nullExit, &decoded); err == nil {
		t.Fatal("json.Unmarshal(null exit_code) succeeded")
	} else {
		requireInvalidJSON(t, err)
	}

	oversized := []byte("{\"notes\":\"" + strings.Repeat("x", MaxEvidenceJSONBytes) + "\"}")
	if err := json.Unmarshal(oversized, &decoded); err == nil {
		t.Fatal("json.Unmarshal(oversized Evidence) succeeded")
	} else {
		requireInvalidJSON(t, err)
	}

	var nilEvidence *Evidence
	if err := nilEvidence.UnmarshalJSON(raw); !errors.Is(err, ErrInvalidEvidenceJSON) {
		t.Fatalf("nil Evidence.UnmarshalJSON() error = %v, want ErrInvalidEvidenceJSON", err)
	}
}
