package livegate

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func requireInvalidCandidateJSON(t *testing.T, err error) {
	t.Helper()
	if err != ErrInvalidCandidateJSON {
		t.Fatalf("error = %v, want fixed ErrInvalidCandidateJSON", err)
	}
}

func TestProjectCandidateJSONSizeBoundary(t *testing.T) {
	base := []byte(`{"value":0}`)
	exact := append(append([]byte(nil), base...),
		bytes.Repeat([]byte{' '}, MaxCandidateJSONBytes-len(base))...)
	if len(exact) != MaxCandidateJSONBytes {
		t.Fatalf("exact size = %d", len(exact))
	}
	if err := ProjectCandidateJSON(bytes.NewReader(exact), nil); err != nil {
		t.Fatalf("exact limit rejected: %v", err)
	}

	oversized := append(append([]byte(nil), exact...), ' ')
	requireInvalidCandidateJSON(t, ProjectCandidateJSON(bytes.NewReader(oversized), nil))
}

func TestProjectCandidateJSONRejectsNestedDuplicateKeys(t *testing.T) {
	tests := []string{
		`{"outer":{"key":1,"key":2}}`,
		`{"outer":{"key":1,"\u006bey":2}}`,
		`{"items":[{"key":1,"key":2}]}`,
	}
	for _, input := range tests {
		requireInvalidCandidateJSON(t, ProjectCandidateJSON(strings.NewReader(input), nil))
	}
}

func TestProjectCandidateJSONTrailingInput(t *testing.T) {
	if err := ProjectCandidateJSON(strings.NewReader("{\"value\":1} \r\n\t"), nil); err != nil {
		t.Fatalf("trailing whitespace rejected: %v", err)
	}
	for _, input := range []string{
		`{"value":1}{"second":2}`,
		`{"value":1} trailing`,
	} {
		requireInvalidCandidateJSON(t, ProjectCandidateJSON(strings.NewReader(input), nil))
	}
}

func TestProjectCandidateJSONRejectsInvalidUTF8(t *testing.T) {
	rawInvalid := append([]byte(`{"value":"`), 0xff)
	rawInvalid = append(rawInvalid, []byte(`"}`)...)

	tests := [][]byte{
		rawInvalid,
		[]byte(`{"value":"\ud800"}`),
		[]byte(`{"\ud800":1}`),
	}
	for index, input := range tests {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			requireInvalidCandidateJSON(t, ProjectCandidateJSON(bytes.NewReader(input), nil))
		})
	}
}

func TestProjectCandidateJSONDepthLimit(t *testing.T) {
	atLimit := strings.Repeat("[", MaxCandidateJSONDepth) +
		"0" +
		strings.Repeat("]", MaxCandidateJSONDepth)
	if err := ProjectCandidateJSON(strings.NewReader(atLimit), nil); err != nil {
		t.Fatalf("depth limit rejected: %v", err)
	}

	overLimit := "[" + atLimit + "]"
	requireInvalidCandidateJSON(t, ProjectCandidateJSON(strings.NewReader(overLimit), nil))
}

func TestProjectCandidateJSONCallbackProjection(t *testing.T) {
	input := `{
		"schema_version": 1,
		"live_gate_targets": [
			{
				"platform": "linux-amd64",
				"size": 12,
				"enabled": true,
				"unknown": {"nested": "ignored"}
			},
			{
				"platform": "windows-amd64",
				"size": 34
			}
		],
		"future": null
	}`

	projected := make(map[string]string)
	var retainedPaths [][]string
	callbacks := 0
	err := ProjectCandidateJSON(strings.NewReader(input), func(path []string, scalar any) error {
		callbacks++
		retainedPaths = append(retainedPaths, path)
		key := strings.Join(path, "/")
		switch key {
		case "schema_version",
			"live_gate_targets/0/platform",
			"live_gate_targets/0/size",
			"live_gate_targets/1/platform",
			"live_gate_targets/1/size":
			projected[key] = fmt.Sprint(scalar)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ProjectCandidateJSON() error = %v", err)
	}
	if callbacks != 8 {
		t.Fatalf("callback count = %d, want scalar-only 8", callbacks)
	}
	want := map[string]string{
		"schema_version":               "1",
		"live_gate_targets/0/platform": "linux-amd64",
		"live_gate_targets/0/size":     "12",
		"live_gate_targets/1/platform": "windows-amd64",
		"live_gate_targets/1/size":     "34",
	}
	if !reflect.DeepEqual(projected, want) {
		t.Fatalf("projection = %#v, want %#v", projected, want)
	}
	if len(retainedPaths) != callbacks {
		t.Fatalf("retained path count = %d", len(retainedPaths))
	}
	retainedPaths[0][0] = "mutated"
	if strings.Join(retainedPaths[1], "/") != "live_gate_targets/0/platform" {
		t.Fatal("callback paths shared mutable storage")
	}

	var numberWasJSONNumber bool
	err = ProjectCandidateJSON(strings.NewReader(`{"number":12}`), func(_ []string, scalar any) error {
		_, numberWasJSONNumber = scalar.(json.Number)
		return nil
	})
	if err != nil || !numberWasJSONNumber {
		t.Fatalf("number projection type was not json.Number: err=%v", err)
	}
}

func TestProjectCandidateJSONRejectsInvalidDocumentShapes(t *testing.T) {
	for _, input := range []string{
		"",
		"{",
		"[1,",
		`{"value":}`,
	} {
		requireInvalidCandidateJSON(t, ProjectCandidateJSON(strings.NewReader(input), nil))
	}
	requireInvalidCandidateJSON(t, ProjectCandidateJSON(nil, nil))
}

func TestProjectCandidateJSONInternalStructureEvents(t *testing.T) {
	input := `{"empty":{},"items":[],"null":null,"value":1}`
	type observedNode struct {
		kind   candidateJSONNodeKind
		scalar any
	}
	observed := make(map[string]observedNode)
	err := projectCandidateJSONNodes(strings.NewReader(input), func(path []string, node candidateJSONNode) error {
		observed[strings.Join(path, "/")] = observedNode{kind: node.kind, scalar: node.scalar}
		return nil
	})
	if err != nil {
		t.Fatalf("projectCandidateJSONNodes() error = %v", err)
	}
	if got := observed[""]; got.kind != candidateJSONObject {
		t.Fatalf("root event = %#v", got)
	}
	if got := observed["empty"]; got.kind != candidateJSONObject {
		t.Fatalf("empty object event = %#v", got)
	}
	if got := observed["items"]; got.kind != candidateJSONArray {
		t.Fatalf("empty array event = %#v", got)
	}
	if got := observed["null"]; got.kind != candidateJSONScalar || got.scalar != nil {
		t.Fatalf("null scalar event = %#v", got)
	}
	if got := observed["value"]; got.kind != candidateJSONScalar {
		t.Fatalf("number scalar event = %#v", got)
	} else if number, ok := got.scalar.(json.Number); !ok || number.String() != "1" {
		t.Fatalf("number scalar = %#v", got.scalar)
	}
	if len(observed) != 5 {
		t.Fatalf("event count = %d, want 5", len(observed))
	}
}

func TestProjectCandidateJSONFixedErrorDoesNotLeak(t *testing.T) {
	const canary = "CANDIDATE_JSON_SECRET_CANARY"

	duplicate := fmt.Sprintf(`{"%s":1,"%s":2}`, canary, canary)
	err := ProjectCandidateJSON(strings.NewReader(duplicate), nil)
	requireInvalidCandidateJSON(t, err)
	if strings.Contains(err.Error(), canary) {
		t.Fatal("parse error leaked candidate JSON")
	}

	err = ProjectCandidateJSON(strings.NewReader(`{"value":1}`), func([]string, any) error {
		return fmt.Errorf("callback rejected %s", canary)
	})
	requireInvalidCandidateJSON(t, err)
	if strings.Contains(err.Error(), canary) {
		t.Fatal("callback error leaked through fixed sentinel")
	}
}
