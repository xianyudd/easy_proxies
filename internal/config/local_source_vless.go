package config

import (
	"net/url"
	"strconv"
)

// validateVLESSURI performs structural checks on an already-parsed vless://
// URI. The userinfo must be exactly one canonical UUID (36 chars, hyphens at
// the RFC 4122 positions, hex digits elsewhere) with no password component.
// Any explicit port must be in 1..65535. No dangerous control characters may
// survive in any URI component. Errors are sanitized: they carry the original
// input line number and never echo the URI or its credentials.
func validateVLESSURI(parsed *url.URL, lineNo int) error {
	if parsed.User == nil {
		return localParseError(lineNo, "vless URI is missing UUID userinfo")
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return localParseError(lineNo, "vless userinfo must be a bare UUID without password")
	}
	if !isCanonicalUUID(parsed.User.Username()) {
		return localParseError(lineNo, "vless userinfo is not a canonical UUID")
	}
	if port := parsed.Port(); port != "" {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			return localParseError(lineNo, "vless URI has an invalid port")
		}
	}
	for _, part := range []string{parsed.User.String(), parsed.Host, parsed.RawQuery, parsed.Fragment} {
		if containsDangerousControlChars(part) {
			return localParseError(lineNo, "vless URI contains control characters")
		}
	}
	return nil
}

// isCanonicalUUID reports whether s is a canonical textual UUID per RFC 4122:
// 36 characters, hyphens at positions 8/13/18/23, lowercase or uppercase hex
// digits elsewhere (16 bytes of hex in total).
func isCanonicalUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch i {
		case 8, 13, 18, 23:
			continue
		}
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// containsDangerousControlChars reports whether s contains C0 control
// characters or DEL, which must never ride along inside connection text.
func containsDangerousControlChars(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}
