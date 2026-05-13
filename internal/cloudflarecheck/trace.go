package cloudflarecheck

import "strings"

func ParseTrace(text string) Trace {
	var trace Trace
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "ip":
			trace.IP = val
		case "loc":
			trace.LOC = strings.ToUpper(val)
		case "colo":
			trace.COLO = strings.ToUpper(val)
		case "http":
			trace.HTTP = val
		case "tls":
			trace.TLS = val
		case "warp":
			trace.WARP = val
		}
	}
	return trace
}
