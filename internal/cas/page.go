package cas

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	publicKeyRE = regexp.MustCompile(`(?i)(?:publicKeyStr|publicKey)\s*=\s*["']([A-Za-z0-9+/=]+)["']`)
	qrUUIDRE    = regexp.MustCompile(`(?i)(?:uuid|qrCodeUUID|qrcode_uuid)\s*[:=]\s*["']([A-Za-z0-9._-]{8,128})["']`)
)

const (
	minimumPublicKeyLength = 128
	maximumPublicKeyLength = 8192
)

// ParsePage discovers the current CAS form, public scripts, QR capability, and
// active challenge without assuming that those values are protocol constants.
func ParsePage(base *url.URL, data []byte) (Page, error) {
	result := Page{Hidden: make(url.Values)}
	if base == nil {
		return result, ErrUnrecognized
	}
	challenge, err := DetectChallenge(data)
	if err != nil {
		return result, ErrUnrecognized
	}
	result.Challenge = challenge

	document, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return result, ErrUnrecognized
	}
	form := document.Find("form#loginForm").First()
	if form.Length() == 0 {
		form = document.Find("form[action]").First()
	}
	if form.Length() > 0 {
		actionValue, _ := form.Attr("action")
		if actionValue == "" {
			actionValue = base.String()
		}
		action, resolveErr := base.Parse(actionValue)
		if resolveErr != nil {
			return result, ErrUnrecognized
		}
		result.Action = action
		form.Find("input[name]").Each(func(_ int, selection *goquery.Selection) {
			name, _ := selection.Attr("name")
			value, _ := selection.Attr("value")
			if name != "" {
				result.Hidden.Add(name, value)
			}
		})
	}

	result.PublicKey = extractPublicKey(data)
	if match := qrUUIDRE.FindSubmatch(data); len(match) == 2 {
		result.QRUUID = string(match[1])
	}
	lowerHTML := strings.ToLower(string(data))
	if strings.Contains(lowerHTML, "qyqrlogin") && strings.Contains(lowerHTML, "checkqrcodescan") {
		result.QRSupported = true
	}
	document.Find("script[src]").Each(func(_ int, selection *goquery.Selection) {
		source, _ := selection.Attr("src")
		resolved, resolveErr := base.Parse(source)
		if resolveErr == nil {
			result.Scripts = append(result.Scripts, resolved)
			if strings.Contains(strings.ToLower(resolved.Path), "login-qrcode") {
				result.QRSupported = true
			}
		}
	})
	return result, nil
}

func ExtractPublicKey(script []byte) string {
	return extractPublicKey(script)
}

func extractPublicKey(data []byte) string {
	match := publicKeyRE.FindSubmatch(data)
	if len(match) != 2 || len(match[1]) < minimumPublicKeyLength || len(match[1]) > maximumPublicKeyLength {
		return ""
	}
	return string(match[1])
}
