package cas

import (
	"errors"
	"strings"
	"testing"
)

func TestChallengeIgnoresDormantControlsOnPasswordPage(t *testing.T) {
	page := []byte(`<html>
		<script>const dormant = "手机验证码";</script>
		<template><form><input name="smsCode">短信验证码</form></template>
		<form id="loginForm">
			<input type="hidden" name="lt" value="synthetic-state">
			<input type="hidden" name="execution" value="e1s1">
		</form>
		<div hidden><input name="smsCode"><input name="trustDevice"></div>
	</html>`)
	challenge, err := DetectChallenge(page)
	if err != nil || challenge != ChallengeNone {
		t.Fatalf("ordinary password page = %q, %v", challenge, err)
	}
}

func TestChallengeClassifiesHTMLBeforeInlineJavaScript(t *testing.T) {
	passwordForm := `<form id="loginForm"><input type="hidden" name="lt" value="synthetic-state"><input type="hidden" name="execution" value="e1s1"></form>`
	page := []byte("\uFEFF  <html>" + passwordForm + `<script>
		function initialize() { window.syntheticConfiguration = { enabled: true }; }
		initialize();
	</script></html>`)
	challenge, err := DetectChallenge(page)
	if err != nil || challenge != ChallengeNone {
		t.Fatalf("ordinary HTML with inline JavaScript = %q, %v", challenge, err)
	}
	failure, err := DetectAuthenticationFailure(page)
	if err != nil || failure {
		t.Fatalf("ordinary HTML failure classification = %v, %v", failure, err)
	}
}

func TestChallengeStillDetectsActiveHTMLBeforeInlineJavaScript(t *testing.T) {
	page := []byte(`<html>
		<form id="loginForm"><input name="lt"><input name="execution"></form>
		<form id="smsVerify"><input name="smsCode"></form>
		<script>function initialize() { return { enabled: true }; }</script>
	</html>`)
	challenge, err := DetectChallenge(page)
	if err != nil || challenge != ChallengeSMSOTP {
		t.Fatalf("active HTML challenge with inline JavaScript = %q, %v", challenge, err)
	}
}

func TestChallengeTakesPriorityOverRetainedPasswordState(t *testing.T) {
	page := []byte(`<html>
		<form id="loginForm">
			<input type="hidden" name="lt" value="synthetic-state">
			<input type="hidden" name="execution" value="e1s1">
		</form>
		<form id="smsVerify"><label>短信验证码<input name="smsCode"></label></form>
	</html>`)
	challenge, err := DetectChallenge(page)
	if err != nil || challenge != ChallengeSMSOTP {
		t.Fatalf("active OTP with retained CAS state = %q, %v", challenge, err)
	}
}

func TestChallengeIgnoresDisabledOTPWithRetainedPasswordState(t *testing.T) {
	passwordForm := `<form id="loginForm"><input type="hidden" name="lt" value="synthetic-state"><input type="hidden" name="execution" value="e1s1"></form>`
	for _, dormant := range []string{
		`<input name="smsCode" disabled>`,
		`<fieldset disabled><label>短信验证码<input name="otpCode"></label></fieldset>`,
	} {
		challenge, err := DetectChallenge([]byte(`<html>` + passwordForm + dormant + `</html>`))
		if err != nil || challenge != ChallengeNone {
			t.Fatalf("disabled OTP control = %q, %v", challenge, err)
		}
	}
}

func TestAuthenticationFailureUsesOnlyActiveDOM(t *testing.T) {
	passwordForm := `<form id="loginForm"><input type="hidden" name="lt" value="state"><input type="hidden" name="execution" value="e1s1"></form>`
	dormant := []byte(`<html>` + passwordForm + `
		<script>const message = "用户名或密码不正确";</script>
		<template><p>用户名或密码不正确</p></template>
		<div hidden role="alert">用户名或密码不正确</div>
		<div aria-hidden="true" role="alert">用户名或密码不正确</div>
		<div style="display: none" role="alert">用户名或密码不正确</div>
	</html>`)
	failure, err := DetectAuthenticationFailure(dormant)
	if err != nil || failure {
		t.Fatalf("dormant error controls = %v, %v", failure, err)
	}

	active := []byte(`<html>` + passwordForm + `<div role="alert">用户名或密码不正确</div></html>`)
	failure, err = DetectAuthenticationFailure(active)
	if err != nil || !failure {
		t.Fatalf("active alert = %v, %v", failure, err)
	}

	visibleWithoutForm := []byte(`<html><p>invalid username or password</p></html>`)
	failure, err = DetectAuthenticationFailure(visibleWithoutForm)
	if err != nil || !failure {
		t.Fatalf("visible no-form error = %v, %v", failure, err)
	}

	generic, err := DetectAuthenticationFailure([]byte(`<p>请求参数不正确</p>`))
	if err != nil || generic {
		t.Fatalf("generic parameter error = %v, %v", generic, err)
	}
}

func TestStructuredChallengeIsStrict(t *testing.T) {
	for _, input := range []string{
		`{"message":"需要短信验证码","message":"需要短信验证码"}`,
		`{"challenge_kind":"sms_otp","type":"device_verification"}`,
		`{"message":1}`,
		`window.callback({"challenge_kind":"sms_otp"})`,
		`callback({"challenge_kind":"sms_otp"}); alert(1)`,
	} {
		if _, err := DetectChallenge([]byte(input)); !errors.Is(err, ErrUnrecognized) {
			t.Fatalf("DetectChallenge(%s) error = %v", input, err)
		}
	}

	challenge, err := DetectChallenge([]byte(`{"challenge_kind":"webauthn"}`))
	if err != nil || challenge != ChallengeUnknown {
		t.Fatalf("unknown challenge = %q, %v", challenge, err)
	}

	challenge, err = DetectChallenge([]byte(`{"challenge_kind":"sms_otp","type":"smsotp"}`))
	if err != nil || challenge != ChallengeSMSOTP {
		t.Fatalf("equivalent challenge aliases = %q, %v", challenge, err)
	}

	challenge, err = DetectChallenge([]byte(`callback({"challenge_kind":"sms_otp"});`))
	if err != nil || challenge != ChallengeSMSOTP {
		t.Fatalf("strict JSONP challenge = %q, %v", challenge, err)
	}
}

func TestChallengeParseErrorsNeverContainResponseContent(t *testing.T) {
	const canary = "CAS-CHALLENGE-RESPONSE-SECRET-CANARY"
	response := []byte(`callback({"message":"` + canary + `","message":"` + canary + `"})`)
	if _, err := DetectChallenge(response); !errors.Is(err, ErrUnrecognized) || strings.Contains(err.Error(), canary) {
		t.Fatalf("challenge parse error leaked response content: %v", err)
	}
	if _, err := DetectAuthenticationFailure(response); !errors.Is(err, ErrUnrecognized) || strings.Contains(err.Error(), canary) {
		t.Fatalf("authentication failure parse error leaked response content: %v", err)
	}
}
