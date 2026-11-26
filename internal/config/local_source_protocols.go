package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// These checks are deliberately pure: preview must not resolve DNS, fetch ECH,
// construct a proxy, or bind a port. They cover the formats accepted by builder.
func validateLocalProtocol(raw string, line int) error {
	fail := func() error { return localParseError(line, "invalid proxy structure or unsupported option") }
	scheme, err := localSourceURIScheme(raw, line)
	if err != nil {
		return err
	}
	if scheme == "vmess" && !strings.Contains(strings.SplitN(raw, "?", 2)[0], "@") {
		if err := validateLocalVMessJSON(strings.TrimPrefix(raw, "vmess://")); err != nil {
			return fail()
		}
		return nil
	}
	normalized := raw
	if scheme == "hy2" || scheme == "hysteria2" {
		// The builder supports a port set in the authority as well as query aliases.
		end := len(raw)
		if i := strings.IndexAny(raw, "?#"); i >= 0 {
			end = i
		}
		start := strings.LastIndex(raw[:end], "@")
		colon := strings.LastIndex(raw[:end], ":")
		if start >= 0 && colon > start && !strings.Contains(raw[colon:end], "]") {
			port := raw[colon+1 : end]
			if strings.ContainsAny(port, "-,:") {
				if !validLocalPortSet(port) {
					return fail()
				}
				normalized = raw[:colon+1] + "443" + raw[end:]
			}
		}
	}
	u, err := url.Parse(normalized)
	if err != nil || !validLocalHost(u.Hostname()) {
		return fail()
	}
	if u.Port() != "" && !validLocalPort(u.Port()) {
		return fail()
	}
	if strings.HasSuffix(u.Host, ":") {
		return fail()
	}
	decoded, err := url.PathUnescape(normalized)
	if err != nil || containsDangerousControlChars(decoded) {
		return fail()
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return fail()
	}
	switch scheme {
	case "vless", "vmess":
		if err := validateVLESSURI(u, line); err != nil {
			return err
		}
	case "trojan":
		if u.User == nil || u.User.Username() == "" {
			return fail()
		}
	case "anytls", "hy2", "hysteria2":
		if u.User == nil {
			return fail()
		}
		pass, _ := u.User.Password()
		if u.User.Username() == "" && pass == "" {
			return fail()
		}
	case "tuic":
		if u.User == nil || !isCanonicalUUID(u.User.Username()) {
			return fail()
		}
		pass, _ := u.User.Password()
		if pass == "" {
			return fail()
		}
	case "ss", "shadowsocks":
		if u.User == nil || q.Get("plugin") != "" {
			return fail()
		}
		b, err := decodeLocalBase64(u.User.String())
		if err != nil {
			return fail()
		}
		method, pass, ok := strings.Cut(string(b), ":")
		if !ok || pass == "" || containsDangerousControlChars(string(b)) {
			return fail()
		}
		switch method {
		case "aes-128-gcm", "aes-192-gcm", "aes-256-gcm", "chacha20-ietf-poly1305", "chacha20-poly1305", "xchacha20-ietf-poly1305", "2022-blake3-aes-128-gcm", "2022-blake3-aes-256-gcm", "2022-blake3-chacha20-poly1305":
		default:
			return fail()
		}
	case "http", "https", "socks", "socks5", "socks5h":
	default:
		return fail()
	}
	if scheme == "hy2" || scheme == "hysteria2" {
		for _, key := range []string{"ports", "server_ports", "mport"} {
			if v := q.Get(key); v != "" && !validLocalPortSet(v) {
				return fail()
			}
		}
		for _, key := range []string{"hop_interval", "hopInterval"} {
			if v := q.Get(key); v != "" {
				d, e := time.ParseDuration(v)
				if e != nil || d <= 0 {
					return fail()
				}
			}
		}
	}
	return nil
}

func decodeLocalBase64(s string) ([]byte, error) {
	b, e := base64.StdEncoding.DecodeString(s)
	if e == nil {
		return b, nil
	}
	return base64.RawURLEncoding.DecodeString(s)
}
func validLocalPort(s string) bool { n, e := strconv.Atoi(s); return e == nil && n > 0 && n <= 65535 }
func validLocalHost(s string) bool {
	if s == "" || strings.ContainsAny(s, "/@?#[]") || strings.IndexFunc(s, unicode.IsSpace) >= 0 || containsDangerousControlChars(s) {
		return false
	}
	return !strings.Contains(s, ":") || net.ParseIP(s) != nil
}
func validLocalPortSet(s string) bool {
	for _, part := range strings.Split(s, ",") {
		bounds := strings.Split(strings.ReplaceAll(part, ":", "-"), "-")
		if len(bounds) > 2 || !validLocalPort(bounds[0]) {
			return false
		}
		if len(bounds) == 2 {
			if !validLocalPort(bounds[1]) {
				return false
			}
			a, _ := strconv.Atoi(bounds[0])
			b, _ := strconv.Atoi(bounds[1])
			if a > b {
				return false
			}
		}
	}
	return true
}
func validateLocalVMessJSON(encoded string) error {
	bad := errors.New("invalid vmess structure")
	b, err := decodeLocalBase64(encoded)
	if err != nil {
		return bad
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(b, &obj) != nil || obj == nil {
		return bad
	}
	var host, id string
	if json.Unmarshal(obj["add"], &host) != nil || !validLocalHost(host) || json.Unmarshal(obj["id"], &id) != nil || !isCanonicalUUID(id) {
		return bad
	}
	for key, value := range obj {
		if key == "port" || key == "aid" {
			var text string
			if json.Unmarshal(value, &text) != nil {
				text = string(value)
			}
			n, e := strconv.Atoi(text)
			if e != nil || (key == "port" && (n < 1 || n > 65535)) || (key == "aid" && n < 0) {
				return bad
			}
		} else {
			var text string
			if json.Unmarshal(value, &text) != nil || containsDangerousControlChars(text) {
				return bad
			}
		}
	}
	return nil
}
