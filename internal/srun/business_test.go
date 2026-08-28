package srun

import (
	"errors"
	"strings"
	"testing"
)

func TestParseActivationSuccess(t *testing.T) {
	for _, input := range []string{
		`{"code":0,"message":"success"}`,
		`callback_1({"code":0,"message":"success","success":true});`,
	} {
		success, err := ParseActivationSuccess([]byte(input))
		if err != nil || !success {
			t.Fatalf("activation success = %v, %v", success, err)
		}
	}

	for _, input := range []string{
		`{"code":1,"message":"invalid credentials"}`,
		`{"code":2,"message":"failed","success":false}`,
	} {
		success, err := ParseActivationSuccess([]byte(input))
		if err != nil || success {
			t.Fatalf("explicit activation failure = %v, %v", success, err)
		}
	}
}

func TestParseActivationRejectsUnknownOrConflictingResponses(t *testing.T) {
	for _, input := range []string{
		`{"code":0,"message":"failed"}`,
		`{"code":1,"message":"success"}`,
		`{"message":"success"}`,
		`{"code":0}`,
		`{"code":"0","message":"success"}`,
		`{"code":0.0,"message":"success"}`,
		`{"code":0e0,"message":"success"}`,
		`{"code":0,"message":true}`,
		`{"code":0,"code":0,"message":"success"}`,
		`{"code":0,"co\u0064e":0,"message":"success"}`,
		`{"code":0,"message":"success","success":false}`,
		`{"code":1,"message":"failed","success":true}`,
		`{"code":0,"message":"success","success":"true"}`,
		`<html>success</html>`,
		`{}`,
	} {
		requireUnrecognizedBusiness(t, ParseActivationSuccess, input)
	}
}

func TestParseLogoutSuccess(t *testing.T) {
	for _, input := range []string{
		`{"ecode":0}`,
		`callback_1({"ecode":0,"success":true});`,
	} {
		success, err := ParseLogoutSuccess([]byte(input))
		if err != nil || !success {
			t.Fatalf("logout success = %v, %v", success, err)
		}
	}

	for _, input := range []string{
		`{"ecode":1}`,
		`{"ecode":2,"success":false}`,
	} {
		success, err := ParseLogoutSuccess([]byte(input))
		if err != nil || success {
			t.Fatalf("explicit logout failure = %v, %v", success, err)
		}
	}
}

func TestParseLogoutRejectsUnknownOrConflictingResponses(t *testing.T) {
	for _, input := range []string{
		`{}`,
		`{"ecode":"0"}`,
		`{"ecode":0.0}`,
		`{"ecode":0e0}`,
		`{"ecode":0,"ecode":0}`,
		`{"ecode":0,"ec\u006fde":0}`,
		`{"ecode":0,"success":false}`,
		`{"ecode":1,"success":true}`,
		`{"ecode":0,"success":"true"}`,
		`{"code":0,"message":"success"}`,
		`<html>logout success</html>`,
	} {
		requireUnrecognizedBusiness(t, ParseLogoutSuccess, input)
	}
}

func TestBusinessErrorsDoNotLeakResponseBody(t *testing.T) {
	const canary = "SRUN-BUSINESS-RESPONSE-CANARY"
	_, err := ParseActivationSuccess([]byte(`{"message":"` + canary + `"}`))
	if !errors.Is(err, ErrUnrecognized) {
		t.Fatalf("ParseActivationSuccess() error = %v", err)
	}
	if strings.Contains(err.Error(), canary) {
		t.Fatalf("business response leaked through error: %v", err)
	}
}

func requireUnrecognizedBusiness(t *testing.T, parse func([]byte) (bool, error), input string) {
	t.Helper()
	if success, err := parse([]byte(input)); success || !errors.Is(err, ErrUnrecognized) {
		t.Fatalf("business response %q = %v, %v", input, success, err)
	}
}
