package srun

import (
	"encoding/json"
	"regexp"
	"strconv"

	"github.com/UnbalancedCat/ipgw-meta/internal/wirejson"
)

var strictJSONInteger = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)

// ParseActivationSuccess requires the observed Srun activation contract:
// numeric code zero and the exact message "success" must agree.
func ParseActivationSuccess(data []byte) (bool, error) {
	object, err := wirejson.DecodeObjectOrJSONP(data)
	if err != nil {
		return false, ErrUnrecognized
	}
	code, ok := requiredInteger(object, "code")
	if !ok {
		return false, ErrUnrecognized
	}
	message, ok := requiredString(object, "message")
	if !ok {
		return false, ErrUnrecognized
	}
	codeSuccess := code == 0
	messageSuccess := message == "success"
	if codeSuccess != messageSuccess {
		return false, ErrUnrecognized
	}
	success := codeSuccess && messageSuccess
	if err := consistentOptionalSuccess(object, success); err != nil {
		return false, err
	}
	return success, nil
}

// ParseLogoutSuccess requires the observed numeric ecode contract. An object
// without ecode is not a logout result.
func ParseLogoutSuccess(data []byte) (bool, error) {
	object, err := wirejson.DecodeObjectOrJSONP(data)
	if err != nil {
		return false, ErrUnrecognized
	}
	ecode, ok := requiredInteger(object, "ecode")
	if !ok {
		return false, ErrUnrecognized
	}
	success := ecode == 0
	if err := consistentOptionalSuccess(object, success); err != nil {
		return false, err
	}
	return success, nil
}

func requiredInteger(object wirejson.Object, name string) (int64, bool) {
	raw, ok := object.Raw(name)
	if !ok || !strictJSONInteger.Match(raw) {
		return 0, false
	}
	value, err := strconv.ParseInt(string(raw), 10, 64)
	return value, err == nil
}

func requiredString(object wirejson.Object, name string) (string, bool) {
	raw, ok := object.Raw(name)
	if !ok {
		return "", false
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", false
	}
	return value, true
}

func consistentOptionalSuccess(object wirejson.Object, expected bool) error {
	raw, ok := object.Raw("success")
	if !ok {
		return nil
	}
	var stated bool
	if json.Unmarshal(raw, &stated) != nil || stated != expected {
		return ErrUnrecognized
	}
	return nil
}
