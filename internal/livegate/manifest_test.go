package livegate

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

const (
	testLinuxTargetSHA   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testWindowsTargetSHA = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func validCandidateManifestBytes() []byte {
	return []byte("{\"schema_version\":1," +
		"\"plan_id\":\"IPGW-META-V1\"," +
		"\"revision\":\"2026-08-28-r2\"," +
		"\"version\":\"v1.0.0\"," +
		"\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"," +
		"\"source_commit\":\"0123456789abcdef0123456789abcdef01234567\"," +
		"\"live_gate_targets\":[" +
		"{\"platform\":\"linux-amd64\",\"name\":\"ipgw-meta\",\"size\":123456,\"sha256\":\"" + testLinuxTargetSHA + "\"}," +
		"{\"platform\":\"windows-amd64\",\"name\":\"ipgw-meta.exe\",\"size\":234567,\"sha256\":\"" + testWindowsTargetSHA + "\"}" +
		"]}")
}

func TestDecodeCandidateManifestSelectsTargetsAndBindsExactDigest(t *testing.T) {
	raw := validCandidateManifestBytes()
	digest := sha256.Sum256(raw)
	wantSetSHA := hex.EncodeToString(digest[:])
	tests := []struct {
		name     string
		platform Platform
		fileName string
		size     int64
		sha      string
	}{
		{"linux", PlatformLinuxAMD64, "ipgw-meta", 123456, testLinuxTargetSHA},
		{"windows", PlatformWindowsAMD64, "ipgw-meta.exe", 234567, testWindowsTargetSHA},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding, err := DecodeCandidateManifest(bytes.NewReader(raw), test.platform)
			if err != nil {
				t.Fatalf("DecodeCandidateManifest() error = %v", err)
			}
			if binding.CandidateID != "v1.0.0-0123456789ab-12345.1" ||
				binding.CandidateSetSHA256 != wantSetSHA ||
				binding.SourceCommit != testSourceCommit ||
				binding.Platform != test.platform ||
				binding.Name != test.fileName ||
				binding.Size != test.size ||
				binding.SHA256 != test.sha {
				t.Fatalf("binding = %#v", binding)
			}
		})
	}
}

func TestCandidateManifestAllowsAdditiveTopLevelFields(t *testing.T) {
	object := mustJSONObject(t, validCandidateManifestBytes())
	object["source_tree"] = strings.Repeat("d", 40)
	object["future"] = map[string]any{
		"nested": []any{float64(1), true, nil, map[string]any{"value": "accepted"}},
	}
	raw := mustMarshalJSON(t, object)
	binding, err := DecodeCandidateManifest(bytes.NewReader(raw), PlatformLinuxAMD64)
	if err != nil {
		t.Fatalf("DecodeCandidateManifest() rejected additive fields: %v", err)
	}
	digest := sha256.Sum256(raw)
	if binding.CandidateSetSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("binding digest does not cover exact additive manifest bytes")
	}
}

func TestCandidateManifestRejectsMissingRequiredFields(t *testing.T) {
	raw := validCandidateManifestBytes()
	for _, field := range candidateManifestRequiredFields {
		field := field
		t.Run(field, func(t *testing.T) {
			object := mustJSONObject(t, raw)
			delete(object, field)
			requireInvalidCandidateManifest(t, decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64))
		})
	}
}

func TestCandidateManifestTargetsAreClosedCompleteAndOrdered(t *testing.T) {
	raw := validCandidateManifestBytes()
	for _, field := range candidateTargetFields {
		field := field
		t.Run("missing_"+field, func(t *testing.T) {
			object := mustJSONObject(t, raw)
			targets := mustJSONArrayField(t, object, "live_gate_targets")
			delete(targets[0].(map[string]any), field)
			requireInvalidCandidateManifest(t, decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64))
		})
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"unknown_target_field", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["future"] = true
		}},
		{"one_target", func(object map[string]any) {
			targets := mustJSONArrayField(t, object, "live_gate_targets")
			object["live_gate_targets"] = targets[:1]
		}},
		{"three_targets", func(object map[string]any) {
			targets := mustJSONArrayField(t, object, "live_gate_targets")
			object["live_gate_targets"] = append(targets, targets[1])
		}},
		{"reversed_order", func(object map[string]any) {
			targets := mustJSONArrayField(t, object, "live_gate_targets")
			targets[0], targets[1] = targets[1], targets[0]
		}},
		{"target_not_object", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[0] = "linux-amd64"
		}},
		{"wrong_linux_name", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["name"] = "ipgw-meta.exe"
		}},
		{"wrong_windows_name", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[1].(map[string]any)["name"] = "ipgw-meta"
		}},
		{"duplicate_platform", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[1].(map[string]any)["platform"] = "linux-amd64"
		}},
		{"platform_case", func(object map[string]any) {
			mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["platform"] = "Linux-amd64"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			object := mustJSONObject(t, raw)
			test.mutate(object)
			requireInvalidCandidateManifest(t, decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64))
		})
	}
}

func TestCandidateManifestRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	raw := validCandidateManifestBytes()
	top := replaceOnce(t, raw,
		[]byte("{\"schema_version\":1"),
		[]byte("{\"plan_id\":\"IPGW-META-V1\",\"schema_version\":1"))
	escapedTop := replaceOnce(t, raw,
		[]byte("{\"schema_version\":1"),
		[]byte("{\"plan\\u005fid\":\"IPGW-META-V1\",\"schema_version\":1"))
	target := replaceOnce(t, raw,
		[]byte("{\"platform\":\"linux-amd64\","),
		[]byte("{\"platform\":\"linux-amd64\",\"platform\":\"linux-amd64\","))

	end := bytes.LastIndexByte(raw, '}')
	if end < 0 {
		t.Fatal("fixture has no final object delimiter")
	}
	nested := append([]byte(nil), raw[:end]...)
	nested = append(nested, []byte(",\"future\":{\"level\":[{\"secret\":1,\"secret\":2}]}}")...)
	nested = append(nested, raw[end:]...)

	for name, candidate := range map[string][]byte{
		"top":            top,
		"escaped_top":    escapedTop,
		"target":         target,
		"unknown_nested": nested,
	} {
		t.Run(name, func(t *testing.T) {
			requireInvalidCandidateManifest(t, decodeCandidateManifest(candidate, PlatformLinuxAMD64))
		})
	}
}

func TestCandidateManifestIdentityTypesAndCandidateID(t *testing.T) {
	raw := validCandidateManifestBytes()
	tests := []struct {
		name string
		old  string
		new  string
	}{
		{"schema_value", "\"schema_version\":1", "\"schema_version\":2"},
		{"schema_string", "\"schema_version\":1", "\"schema_version\":\"1\""},
		{"schema_decimal", "\"schema_version\":1", "\"schema_version\":1.0"},
		{"schema_exponent", "\"schema_version\":1", "\"schema_version\":1e0"},
		{"schema_null", "\"schema_version\":1", "\"schema_version\":null"},
		{"plan_case", "\"plan_id\":\"IPGW-META-V1\"", "\"plan_id\":\"ipgw-meta-v1\""},
		{"plan_type", "\"plan_id\":\"IPGW-META-V1\"", "\"plan_id\":1"},
		{"revision", "\"revision\":\"2026-08-28-r2\"", "\"revision\":\"2026-08-28-R2\""},
		{"version", "\"version\":\"v1.0.0\"", "\"version\":\"1.0.0\""},
		{"candidate_shape", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"", "\"candidate_id\":\"v1.0.0-0123456789ab-12345\""},
		{"candidate_sha", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"", "\"candidate_id\":\"v1.0.0-ffffffffffff-12345.1\""},
		{"candidate_run_zero", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"", "\"candidate_id\":\"v1.0.0-0123456789ab-0.1\""},
		{"candidate_run_leading_zero", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"", "\"candidate_id\":\"v1.0.0-0123456789ab-012345.1\""},
		{"candidate_attempt_zero", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.1\"", "\"candidate_id\":\"v1.0.0-0123456789ab-12345.0\""},
		{"source_short", "\"source_commit\":\"0123456789abcdef0123456789abcdef01234567\"", "\"source_commit\":\"0123456789abcdef0123456789abcdef0123456\""},
		{"source_upper", "\"source_commit\":\"0123456789abcdef0123456789abcdef01234567\"", "\"source_commit\":\"0123456789ABCDEF0123456789ABCDEF01234567\""},
		{"targets_null", "\"live_gate_targets\":[", "\"live_gate_targets\":null,\"discarded\":["},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := replaceOnce(t, raw, []byte(test.old), []byte(test.new))
			requireInvalidCandidateManifest(t, decodeCandidateManifest(candidate, PlatformLinuxAMD64))
		})
	}
	requireInvalidCandidateManifest(t, decodeCandidateManifest(raw, Platform("Linux-amd64")))
}

func TestCandidateManifestTargetSizeAndHashBounds(t *testing.T) {
	raw := validCandidateManifestBytes()
	for _, size := range []int64{1, MaxCandidateTargetBytes} {
		t.Run(fmt.Sprintf("valid_%d", size), func(t *testing.T) {
			object := mustJSONObject(t, raw)
			mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["size"] = size
			if _, err := DecodeCandidateManifest(bytes.NewReader(mustMarshalJSON(t, object)), PlatformLinuxAMD64); err != nil {
				t.Fatalf("DecodeCandidateManifest() rejected boundary size: %v", err)
			}
		})
	}
	for i, size := range []any{0, -1, MaxCandidateTargetBytes + 1, "1", nil, 1.5} {
		t.Run(fmt.Sprintf("invalid_size_%d", i), func(t *testing.T) {
			object := mustJSONObject(t, raw)
			mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["size"] = size
			requireInvalidCandidateManifest(t, decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64))
		})
	}
	for _, replacement := range []string{
		strings.Repeat("b", 63),
		strings.Repeat("b", 65),
		strings.Repeat("B", 64),
		strings.Repeat("g", 64),
	} {
		object := mustJSONObject(t, raw)
		mustJSONArrayField(t, object, "live_gate_targets")[0].(map[string]any)["sha256"] = replacement
		requireInvalidCandidateManifest(t, decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64))
	}
	for _, numeric := range []string{"1.0", "1e0"} {
		candidate := replaceOnce(t, raw, []byte("\"size\":123456"), []byte("\"size\":"+numeric))
		requireInvalidCandidateManifest(t, decodeCandidateManifest(candidate, PlatformLinuxAMD64))
	}
}

func TestCandidateManifestUTF8LimitTrailingAndExactDigest(t *testing.T) {
	raw := validCandidateManifestBytes()
	withWhitespace := append(append([]byte(nil), raw...), []byte(" \t\r\n")...)
	binding, err := DecodeCandidateManifest(bytes.NewReader(withWhitespace), PlatformLinuxAMD64)
	if err != nil {
		t.Fatalf("DecodeCandidateManifest() rejected trailing whitespace: %v", err)
	}
	digest := sha256.Sum256(withWhitespace)
	if binding.CandidateSetSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("digest is not over exact manifest bytes")
	}

	if len(raw) >= MaxCandidateManifestJSONBytes {
		t.Fatalf("fixture size = %d", len(raw))
	}
	exact := append(append([]byte(nil), raw...), bytes.Repeat([]byte{' '}, MaxCandidateManifestJSONBytes-len(raw))...)
	if _, err := DecodeCandidateManifest(bytes.NewReader(exact), PlatformLinuxAMD64); err != nil {
		t.Fatalf("DecodeCandidateManifest() rejected exact 64 KiB input: %v", err)
	}
	requireInvalidCandidateManifest(t, decodeCandidateManifest(append(exact, ' '), PlatformLinuxAMD64))
	requireInvalidCandidateManifest(t, decodeCandidateManifest(append(append([]byte(nil), raw...), []byte(" {}")...), PlatformLinuxAMD64))
	requireInvalidCandidateManifest(t, decodeCandidateManifest(append(append([]byte(nil), raw...), 'x'), PlatformLinuxAMD64))
	requireInvalidCandidateManifest(t, decodeCandidateManifest(nil, PlatformLinuxAMD64))
	requireInvalidCandidateManifest(t, decodeCandidateManifest([]byte(" \t\r\n"), PlatformLinuxAMD64))
	requireInvalidCandidateManifest(t, decodeCandidateManifest(append(append([]byte(nil), raw...), byte(0xff)), PlatformLinuxAMD64))
}

func TestCandidateManifestErrorsAreFixedAndDoNotLeak(t *testing.T) {
	if _, err := DecodeCandidateManifest(nil, PlatformLinuxAMD64); !errors.Is(err, ErrCandidateManifestRead) {
		t.Fatalf("DecodeCandidateManifest(nil) error = %v", err)
	}

	const canary = "CANARY_password_cookie_ticket_41ad?secret=yes"
	underlying := fmt.Errorf("transport %s: %w", canary, io.ErrUnexpectedEOF)
	if _, err := DecodeCandidateManifest(candidateManifestErrorReader{err: underlying}, PlatformLinuxAMD64); !errors.Is(err, ErrCandidateManifestRead) {
		t.Fatalf("read error = %v, want ErrCandidateManifestRead", err)
	} else {
		assertErrorTreeDoesNotContain(t, err, canary)
	}

	object := mustJSONObject(t, validCandidateManifestBytes())
	object["candidate_id"] = canary
	err := decodeCandidateManifest(mustMarshalJSON(t, object), PlatformLinuxAMD64)
	requireInvalidCandidateManifest(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)

	malformed := []byte("{\"future\":{\"secret\":\"" + canary + "\",\"secret\":\"" + canary + "\"}}")
	err = decodeCandidateManifest(malformed, PlatformLinuxAMD64)
	requireInvalidCandidateManifest(t, err)
	assertErrorTreeDoesNotContain(t, err, canary)
}

func decodeCandidateManifest(raw []byte, platform Platform) error {
	_, err := DecodeCandidateManifest(bytes.NewReader(raw), platform)
	return err
}

func requireInvalidCandidateManifest(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected invalid candidate manifest error")
	}
	if !errors.Is(err, ErrInvalidCandidateManifest) {
		t.Fatalf("error = %v, want ErrInvalidCandidateManifest", err)
	}
}

type candidateManifestErrorReader struct {
	err error
}

func (r candidateManifestErrorReader) Read([]byte) (int, error) {
	return 0, r.err
}
func TestCandidateManifestStreamsLargeUnknownValuesAndHashesExactBytes(t *testing.T) {
	base := validCandidateManifestBytes()
	end := bytes.LastIndexByte(base, '}')
	if end < 0 {
		t.Fatal("fixture has no final object delimiter")
	}

	raw := append([]byte(nil), base[:end]...)
	raw = append(raw, []byte(`,"empty":{},"future":{"nested":[{"payload":"`)...)
	raw = append(raw, strings.Repeat("x", 48*1024)...)
	raw = append(raw, []byte("\"}]}} \t\r\n")...)
	if len(raw) >= MaxCandidateManifestJSONBytes {
		t.Fatalf("large additive fixture size = %d", len(raw))
	}

	binding, err := DecodeCandidateManifest(bytes.NewReader(raw), PlatformLinuxAMD64)
	if err != nil {
		t.Fatalf("DecodeCandidateManifest() rejected streamed additive values: %v", err)
	}
	digest := sha256.Sum256(raw)
	if binding.CandidateSetSHA256 != hex.EncodeToString(digest[:]) {
		t.Fatal("digest does not cover exact large additive bytes and trailing whitespace")
	}
}

func TestCandidateManifestRejectsDeepUnknownDuplicate(t *testing.T) {
	value := `{"duplicate":1,"duplicate":2}`
	for range 24 {
		value = `{"next":` + value + `}`
	}

	base := validCandidateManifestBytes()
	end := bytes.LastIndexByte(base, '}')
	if end < 0 {
		t.Fatal("fixture has no final object delimiter")
	}
	raw := append([]byte(nil), base[:end]...)
	raw = append(raw, []byte(`,"future":`+value+`}`)...)
	requireInvalidCandidateManifest(
		t,
		decodeCandidateManifest(raw, PlatformLinuxAMD64),
	)
}

func TestCandidateManifestReadErrorAfterValidBytesIsFixed(t *testing.T) {
	const canary = "candidate-manifest-read-canary"
	underlying := fmt.Errorf("%s: %w", canary, io.ErrUnexpectedEOF)
	reader := &candidateManifestDataErrorReader{
		data: append([]byte(nil), validCandidateManifestBytes()...),
		err:  underlying,
	}

	_, err := DecodeCandidateManifest(reader, PlatformLinuxAMD64)
	if !errors.Is(err, ErrCandidateManifestRead) {
		t.Fatalf("error = %v, want ErrCandidateManifestRead", err)
	}
	assertErrorTreeDoesNotContain(t, err, canary)
}

func TestCandidateManifestRejectsInvalidUTF8InsideUnknownValue(t *testing.T) {
	base := validCandidateManifestBytes()
	end := bytes.LastIndexByte(base, '}')
	if end < 0 {
		t.Fatal("fixture has no final object delimiter")
	}

	invalidUTF8 := append([]byte(nil), base[:end]...)
	invalidUTF8 = append(invalidUTF8, []byte(`,"future":"`)...)
	invalidUTF8 = append(invalidUTF8, byte(0xff))
	invalidUTF8 = append(invalidUTF8, []byte(`"}`)...)
	requireInvalidCandidateManifest(
		t,
		decodeCandidateManifest(invalidUTF8, PlatformLinuxAMD64),
	)

	invalidSurrogate := append([]byte(nil), base[:end]...)
	invalidSurrogate = append(
		invalidSurrogate,
		[]byte(`,"future":"\ud800"}`)...,
	)
	requireInvalidCandidateManifest(
		t,
		decodeCandidateManifest(invalidSurrogate, PlatformLinuxAMD64),
	)
}

type candidateManifestDataErrorReader struct {
	data []byte
	err  error
}

func (reader *candidateManifestDataErrorReader) Read(buffer []byte) (int, error) {
	if len(reader.data) == 0 {
		return 0, reader.err
	}
	n := copy(buffer, reader.data)
	reader.data = reader.data[n:]
	if len(reader.data) == 0 {
		return n, reader.err
	}
	return n, nil
}
