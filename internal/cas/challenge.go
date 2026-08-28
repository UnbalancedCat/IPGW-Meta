package cas

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/UnbalancedCat/ipgw-meta/internal/wirejson"
)

var challengeFields = []string{"message", "msg", "description", "error", "status", "result"}

// DetectChallenge classifies an active CAS challenge. Structured responses
// are decoded strictly; malformed JSON/JSONP is a protocol error rather than
// evidence for a non-challenge state.
func DetectChallenge(data []byte) (Challenge, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return ChallengeNone, nil
	}
	if looksStructured(trimmed) {
		object, err := wirejson.DecodeObjectOrJSONP(data)
		if err != nil {
			return ChallengeNone, ErrUnrecognized
		}
		return structuredChallenge(object)
	}
	if strings.HasPrefix(trimmed, "<") {
		return htmlChallenge(data)
	}
	return challengeFromText(trimmed), nil
}

// DetectAuthenticationFailure recognizes only explicit credential failures.
// Dormant assets and hidden DOM controls are excluded before visible text is
// considered.
func DetectAuthenticationFailure(data []byte) (bool, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return false, nil
	}
	if looksStructured(trimmed) {
		object, err := wirejson.DecodeObjectOrJSONP(data)
		if err != nil {
			return false, ErrUnrecognized
		}
		for _, name := range challengeFields {
			raw, ok := object.Raw(name)
			if !ok {
				continue
			}
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return false, ErrUnrecognized
			}
			if authenticationFailureFromText(value) {
				return true, nil
			}
		}
		return false, nil
	}
	if strings.HasPrefix(trimmed, "<") {
		document, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
		if err != nil {
			return false, ErrUnrecognized
		}
		normalLogin := hasPasswordForm(document)
		sanitizeDOM(document)
		if normalLogin {
			failure := false
			document.Find(`[role="alert"], [aria-live], .auth-error, .login-error, .error-message, .errors, #error, #errorMsg, #msg`).EachWithBreak(func(_ int, selection *goquery.Selection) bool {
				failure = authenticationFailureFromText(selection.Text())
				return !failure
			})
			return failure, nil
		}
		return authenticationFailureFromText(document.Text()), nil
	}
	return authenticationFailureFromText(trimmed), nil
}

func structuredChallenge(object wirejson.Object) (Challenge, error) {
	found := ChallengeNone
	merge := func(candidate Challenge) error {
		if candidate == ChallengeNone {
			return nil
		}
		if found != ChallengeNone && found != candidate {
			return ErrUnrecognized
		}
		found = candidate
		return nil
	}

	for _, name := range object.Names() {
		normalized := normalizeIdentifier(name)
		if normalized == "smscode" || normalized == "otpcode" || normalized == "mobilecode" {
			if err := merge(ChallengeSMSOTP); err != nil {
				return ChallengeNone, err
			}
		}
	}

	kind, present, err := aliasedString(object, "challenge_kind", "challenge", "type")
	if err != nil {
		return ChallengeNone, err
	}
	if present {
		candidate := challengeFromKind(kind)
		if err := merge(candidate); err != nil {
			return ChallengeNone, err
		}
	}

	for _, name := range challengeFields {
		raw, ok := object.Raw(name)
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return ChallengeNone, ErrUnrecognized
		}
		if err := merge(challengeFromText(value)); err != nil {
			return ChallengeNone, err
		}
	}
	return found, nil
}

func challengeFromKind(value string) Challenge {
	switch normalizeIdentifier(value) {
	case "":
		return ChallengeNone
	case "smsotp", "smscode", "otpcode":
		return ChallengeSMSOTP
	case "deviceverification", "trustdevice":
		return ChallengeDevice
	case "accountsetup", "bindphone":
		return ChallengeSetup
	default:
		return ChallengeUnknown
	}
}

func aliasedString(object wirejson.Object, names ...string) (string, bool, error) {
	var result string
	present := false
	for _, name := range names {
		raw, ok := object.Raw(name)
		if !ok {
			continue
		}
		var value string
		if json.Unmarshal(raw, &value) != nil {
			return "", false, ErrUnrecognized
		}
		if present && normalizeIdentifier(result) != normalizeIdentifier(value) {
			return "", false, ErrUnrecognized
		}
		result = value
		present = true
	}
	return result, present, nil
}

func htmlChallenge(data []byte) (Challenge, error) {
	document, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return ChallengeNone, ErrUnrecognized
	}
	normalLogin := hasPasswordForm(document)
	sanitizeDOM(document)

	// A challenge response can retain the ordinary CAS lt/execution fields.
	// Scan active, sanitized controls before treating that password form as the
	// current state. Dormant hidden/template/script controls were removed above.
	explicit := ChallengeNone
	document.Find("form, input, select, textarea").EachWithBreak(func(_ int, selection *goquery.Selection) bool {
		identifier := ""
		for _, attribute := range []string{"name", "id", "class"} {
			if value, ok := selection.Attr(attribute); ok {
				identifier += " " + normalizeIdentifier(value)
			}
		}
		switch {
		case containsAny(identifier, "smscode", "otpcode", "mobilecode", "smsverify", "otpverify"):
			explicit = ChallengeSMSOTP
		case containsAny(identifier, "deviceverification", "deviceverify", "trustdevice"):
			explicit = ChallengeDevice
		case containsAny(identifier, "accountsetup", "bindphone"):
			explicit = ChallengeSetup
		}
		return explicit == ChallengeNone
	})
	if explicit != ChallengeNone {
		return explicit, nil
	}
	if challenge := challengeFromText(document.Text()); challenge != ChallengeNone {
		return challenge, nil
	}
	if normalLogin {
		return ChallengeNone, nil
	}
	return ChallengeNone, nil
}

func sanitizeDOM(document *goquery.Document) {
	document.Find("script, style, template, noscript").Remove()
	// Boolean disabled controls are dormant. Removing disabled containers as
	// whole subtrees also excludes controls inherited from a disabled fieldset.
	document.Find("fieldset[disabled], input[disabled], select[disabled], textarea[disabled], button[disabled], option[disabled], optgroup[disabled]").Remove()
	document.Find("*").Each(func(_ int, selection *goquery.Selection) {
		remove := false
		if _, hidden := selection.Attr("hidden"); hidden {
			remove = true
		}
		if ariaHidden, ok := selection.Attr("aria-hidden"); ok && strings.EqualFold(strings.TrimSpace(ariaHidden), "true") {
			remove = true
		}
		if selection.Is("input") {
			if inputType, ok := selection.Attr("type"); ok && strings.EqualFold(strings.TrimSpace(inputType), "hidden") {
				remove = true
			}
		}
		if style, ok := selection.Attr("style"); ok {
			normalized := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(strings.ToLower(style))
			if strings.Contains(normalized, "display:none") || strings.Contains(normalized, "visibility:hidden") {
				remove = true
			}
		}
		if remove {
			selection.Remove()
		}
	})
}

func hasPasswordForm(document *goquery.Document) bool {
	found := false
	document.Find("form").EachWithBreak(func(_ int, form *goquery.Selection) bool {
		hasLT := false
		hasExecution := false
		form.Find("input[name]").Each(func(_ int, input *goquery.Selection) {
			name, _ := input.Attr("name")
			switch strings.ToLower(strings.TrimSpace(name)) {
			case "lt":
				hasLT = true
			case "execution":
				hasExecution = true
			}
		})
		found = hasLT && hasExecution
		return !found
	})
	return found
}

func challengeFromText(value string) Challenge {
	lower := strings.ToLower(value)
	switch {
	case containsAny(lower, "手机验证码", "短信验证码", "smscode", "sms_code", "otpcode", "otp_code"):
		return ChallengeSMSOTP
	case containsAny(lower, "设备验证", "可信设备", "device verification", "trustdevice"):
		return ChallengeDevice
	case containsAny(lower, "完善账号", "绑定手机号", "account setup"):
		return ChallengeSetup
	default:
		return ChallengeNone
	}
}

func authenticationFailureFromText(value string) bool {
	lower := strings.ToLower(value)
	return containsAny(lower,
		"密码错误",
		"账号或密码错误",
		"用户名或密码错误",
		"账号或密码不正确",
		"用户名或密码不正确",
		"invalid credential",
		"invalid username or password",
		"bad credential",
		"authentication failed",
	)
}

func looksStructured(value string) bool {
	if value == "" {
		return false
	}
	switch value[0] {
	case '{', '[', '"':
		return true
	}
	open := strings.IndexByte(value, '(')
	return open > 0 && strings.Contains(value[open+1:], "{")
}

func normalizeIdentifier(value string) string {
	lower := strings.ToLower(value)
	return strings.NewReplacer("_", "", "-", "", " ", "", ".", "").Replace(lower)
}

func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
