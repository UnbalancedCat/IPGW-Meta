package wirejson

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestDecodeObjectOrJSONPAcceptsStrictObjects(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "JSON", input: `{"z":{"nested":[1,{"ok":true}]},"a":"value"}`},
		{name: "JSONP", input: `safe_$9({"a":1,"z":2});`},
		{name: "JSONP whitespace", input: "  callback \t ( {\"z\": 2, \"a\": 1} ) ; \r\n"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			object, err := DecodeObjectOrJSONP([]byte(testCase.input))
			if err != nil {
				t.Fatalf("DecodeObjectOrJSONP() error = %v", err)
			}
			if names := object.Names(); !reflect.DeepEqual(names, []string{"a", "z"}) {
				t.Fatalf("Names() = %#v", names)
			}
		})
	}
}

func TestObjectDoesNotExposeMutableState(t *testing.T) {
	object, err := DecodeObjectOrJSONP([]byte(`{"value":"original"}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := object.Raw("value")
	if !ok {
		t.Fatal("Raw() did not find value")
	}
	raw[1] = 'X'
	again, ok := object.Raw("value")
	if !ok || string(again) != `"original"` {
		t.Fatalf("Raw() exposed mutable state: %q", again)
	}
	if missing, ok := object.Raw("missing"); ok || missing != nil {
		t.Fatalf("Raw() missing = %q, %v", missing, ok)
	}
	var zero Object
	if names := zero.Names(); len(names) != 0 {
		t.Fatalf("zero Object Names() = %#v", names)
	}
}

func TestDecodeObjectOrJSONPRejectsInvalidEnvelopes(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ``},
		{name: "array", input: `[{"a":1}]`},
		{name: "scalar", input: `"value"`},
		{name: "null", input: `null`},
		{name: "two objects", input: `{"a":1} {"b":2}`},
		{name: "bare object semicolon", input: `{"a":1};`},
		{name: "numeric callback", input: `1callback({"a":1})`},
		{name: "dotted callback", input: `window.callback({"a":1})`},
		{name: "JSONP array", input: `callback([1,2])`},
		{name: "JSONP scalar", input: `callback(true)`},
		{name: "double semicolon", input: `callback({"a":1});;`},
		{name: "trailing script", input: `callback({"a":1}); alert(1)`},
		{name: "trailing call", input: `callback({"a":1})()`},
		{name: "malformed object", input: `callback({"a":})`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeObjectOrJSONP([]byte(testCase.input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("DecodeObjectOrJSONP() error = %v", err)
			}
		})
	}
}

func TestDecodeObjectOrJSONPRejectsDuplicateKeysAtEveryDepth(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "top level", input: `{"key":1,"key":2}`},
		{name: "same value", input: `{"key":1,"key":1}`},
		{name: "escaped equivalent", input: `{"error":1,"err\u006fr":2}`},
		{name: "nested object", input: `{"outer":{"key":1,"key":2}}`},
		{name: "object in array", input: `{"outer":[{"key":1,"key":2}]}`},
		{name: "JSONP nested", input: `callback({"outer":{"key":1,"key":2}})`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := DecodeObjectOrJSONP([]byte(testCase.input)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("DecodeObjectOrJSONP() error = %v", err)
			}
		})
	}
}

func TestDecodeErrorsNeverContainResponseBody(t *testing.T) {
	const canary = "WIRE-RESPONSE-SECRET-CANARY"
	_, err := DecodeObjectOrJSONP([]byte(`{"` + canary + `":1,"` + canary + `":2}`))
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("DecodeObjectOrJSONP() error = %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("response body leaked through error: %v", err)
	}
}
