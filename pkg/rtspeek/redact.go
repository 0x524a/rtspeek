package rtspeek

import (
	"net/url"
	"regexp"
)

// redactedPassword replaces a password in output.
const redactedPassword = "***"

// credentialsInText matches scheme://user:password@ inside free text, such as
// an error message that quotes the URL.
var credentialsInText = regexp.MustCompile(`(?i)\b(rtsps?://[^\s:/@]*):[^\s@/]*@`)

// RedactURL hides the password in an RTSP URL: rtsp://user:pass@host becomes
// rtsp://user:***@host. A URL without a password, or one that does not parse,
// is returned with any credentials still masked by RedactText.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return RedactText(raw)
	}
	if _, ok := u.User.Password(); !ok {
		return raw
	}
	u.User = url.UserPassword(u.User.Username(), redactedPassword)
	// url.UserPassword escapes "*" on some versions; keep the output readable.
	return RedactText(u.String())
}

// RedactText masks passwords in any rtsp:// or rtsps:// URL inside s.
func RedactText(s string) string {
	return credentialsInText.ReplaceAllString(s, "${1}:"+redactedPassword+"@")
}
