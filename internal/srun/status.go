package srun

import (
	"encoding/csv"
	"encoding/json"
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"github.com/UnbalancedCat/ipgw-meta/internal/wirejson"
)

var (
	usernameRE      = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)
	unsignedInteger = regexp.MustCompile(`^[0-9]+$`)
	balanceDecimal  = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
)

// ParseStatus accepts the explicitly supported Srun JSON, JSONP, and legacy
// single-line CSV representations. Unknown or internally conflicting data is
// never interpreted as online or offline.
func ParseStatus(data []byte) (Status, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "not_online_error" {
		return Status{}, nil
	}
	if object, err := wirejson.DecodeObjectOrJSONP(data); err == nil {
		return parseStatusObject(object)
	}

	// Do not reinterpret malformed structured data or HTML as CSV merely
	// because it happens to contain commas.
	if trimmed == "" || strings.ContainsAny(trimmed, "<>{}()[]") {
		return Status{}, ErrUnrecognized
	}
	return parseStatusCSV(trimmed)
}

func parseStatusObject(object wirejson.Object) (Status, error) {
	marker, present, err := aliased(object, parseJSONString, "error", "error_code", "res")
	if err != nil || !present {
		return Status{}, ErrUnrecognized
	}
	switch marker {
	case "not_online_error":
		identityPresent, identityErr := offlineIdentityPresent(object)
		if identityErr != nil || identityPresent {
			return Status{}, ErrUnrecognized
		}
		return Status{}, nil
	case "ok":
		// Continue below; online is only valid with identity and IP evidence.
	default:
		return Status{}, ErrUnrecognized
	}

	username, present, err := aliased(object, parseUsername, "user_name", "username", "user")
	if err != nil || !present || !validUsername(username) {
		return Status{}, ErrUnrecognized
	}
	onlineIP, present, err := aliased(object, parseOnlineIPv4, "online_ip")
	if err != nil || !present {
		return Status{}, ErrUnrecognized
	}

	traffic, trafficPresent, err := aliased(object, parseNonNegativeInteger, "sum_bytes", "used_bytes", "traffic")
	if err != nil {
		return Status{}, ErrUnrecognized
	}
	duration, durationPresent, err := aliased(object, parseNonNegativeInteger, "sum_seconds", "used_seconds", "duration")
	if err != nil {
		return Status{}, ErrUnrecognized
	}
	balance, balancePresent, err := aliased(object, parseBalance, "user_balance", "balance")
	if err != nil {
		return Status{}, ErrUnrecognized
	}
	if trafficPresent != durationPresent || balancePresent && !trafficPresent {
		return Status{}, ErrUnrecognized
	}

	result := Status{Online: true, Username: username, OnlineIP: onlineIP}
	if trafficPresent {
		result.Summary = &Summary{TrafficBytes: traffic, DurationSeconds: duration}
		if balancePresent {
			value := balance
			result.Summary.BalanceMinor = &value
		}
	}
	return result, nil
}

func offlineIdentityPresent(object wirejson.Object) (bool, error) {
	username, usernamePresent, err := aliased(object, parseUsername, "user_name", "username", "user")
	if err != nil {
		return false, ErrUnrecognized
	}
	if usernamePresent && username != "" {
		return true, nil
	}
	onlineIP, ipPresent, err := aliased(object, parseJSONString, "online_ip")
	if err != nil {
		return false, ErrUnrecognized
	}
	if ipPresent && onlineIP != "" {
		return true, nil
	}
	for _, name := range []string{"sum_bytes", "used_bytes", "traffic", "sum_seconds", "used_seconds", "duration", "user_balance", "balance"} {
		if _, present := object.Raw(name); present {
			return true, nil
		}
	}
	return false, nil
}

func parseStatusCSV(value string) (Status, error) {
	reader := csv.NewReader(strings.NewReader(value))
	reader.FieldsPerRecord = -1
	records, err := reader.ReadAll()
	if err != nil || len(records) != 1 || len(records[0]) < 9 {
		return Status{}, ErrUnrecognized
	}
	record := records[0]
	username := strings.TrimSpace(record[0])
	if !validUsername(username) {
		return Status{}, ErrUnrecognized
	}
	onlineIP, err := parseOnlineIPv4Text(strings.TrimSpace(record[8]))
	if err != nil {
		return Status{}, ErrUnrecognized
	}
	traffic, err := parseNonNegativeIntegerText(strings.TrimSpace(record[6]))
	if err != nil {
		return Status{}, ErrUnrecognized
	}
	duration, err := parseNonNegativeIntegerText(strings.TrimSpace(record[7]))
	if err != nil {
		return Status{}, ErrUnrecognized
	}

	result := Status{
		Online:   true,
		Username: username,
		OnlineIP: onlineIP,
		Summary:  &Summary{TrafficBytes: traffic, DurationSeconds: duration},
	}
	if len(record) > 11 && strings.TrimSpace(record[11]) != "" {
		balance, balanceErr := parseBalanceText(strings.TrimSpace(record[11]))
		if balanceErr != nil {
			return Status{}, ErrUnrecognized
		}
		result.Summary.BalanceMinor = &balance
	}
	return result, nil
}

func aliased[T comparable](object wirejson.Object, parse func(json.RawMessage) (T, error), names ...string) (T, bool, error) {
	var result T
	present := false
	for _, name := range names {
		raw, ok := object.Raw(name)
		if !ok {
			continue
		}
		value, err := parse(raw)
		if err != nil {
			var zero T
			return zero, false, ErrUnrecognized
		}
		if present && result != value {
			var zero T
			return zero, false, ErrUnrecognized
		}
		result = value
		present = true
	}
	return result, present, nil
}

func parseJSONString(raw json.RawMessage) (string, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return "", ErrUnrecognized
	}
	return value, nil
}

func parseUsername(raw json.RawMessage) (string, error) {
	if value, err := parseJSONString(raw); err == nil {
		return value, nil
	}
	value, err := rawNumber(raw)
	if err != nil || !unsignedInteger.MatchString(value) {
		return "", ErrUnrecognized
	}
	// Normalize an unquoted integer without accepting a float or exponent.
	integer, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return "", ErrUnrecognized
	}
	return strconv.FormatUint(integer, 10), nil
}

func validUsername(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && usernameRE.MatchString(value)
}

func parseOnlineIPv4(raw json.RawMessage) (netip.Addr, error) {
	value, err := parseJSONString(raw)
	if err != nil {
		return netip.Addr{}, ErrUnrecognized
	}
	return parseOnlineIPv4Text(value)
}

func parseOnlineIPv4Text(value string) (netip.Addr, error) {
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() || !address.IsGlobalUnicast() || address.IsUnspecified() || address.IsLoopback() || address.IsLinkLocalUnicast() || address.IsMulticast() {
		return netip.Addr{}, ErrUnrecognized
	}
	return address, nil
}

func parseNonNegativeInteger(raw json.RawMessage) (int64, error) {
	value, err := decimalScalar(raw)
	if err != nil {
		return 0, ErrUnrecognized
	}
	return parseNonNegativeIntegerText(value)
}

func parseNonNegativeIntegerText(value string) (int64, error) {
	if !unsignedInteger.MatchString(value) {
		return 0, ErrUnrecognized
	}
	integer, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, ErrUnrecognized
	}
	return integer, nil
}

func parseBalance(raw json.RawMessage) (int64, error) {
	value, err := decimalScalar(raw)
	if err != nil {
		return 0, ErrUnrecognized
	}
	return parseBalanceText(value)
}

func parseBalanceText(value string) (int64, error) {
	if !balanceDecimal.MatchString(value) {
		return 0, ErrUnrecognized
	}
	negative := strings.HasPrefix(value, "-")
	unsigned := strings.TrimPrefix(value, "-")
	parts := strings.SplitN(unsigned, ".", 2)
	whole := parts[0]
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 2 {
		for _, digit := range fraction[2:] {
			if digit != '0' {
				return 0, ErrUnrecognized
			}
		}
		fraction = fraction[:2]
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	minorDigits := strings.TrimLeft(whole+fraction, "0")
	if minorDigits == "" {
		minorDigits = "0"
	}
	if negative && minorDigits != "0" {
		minorDigits = "-" + minorDigits
	}
	minor, err := strconv.ParseInt(minorDigits, 10, 64)
	if err != nil {
		return 0, ErrUnrecognized
	}
	return minor, nil
}

func decimalScalar(raw json.RawMessage) (string, error) {
	if value, err := parseJSONString(raw); err == nil {
		return value, nil
	}
	return rawNumber(raw)
}

func rawNumber(raw json.RawMessage) (string, error) {
	var number json.Number
	if json.Unmarshal(raw, &number) != nil {
		return "", ErrUnrecognized
	}
	return number.String(), nil
}
