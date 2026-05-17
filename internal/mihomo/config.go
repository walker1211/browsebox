package mihomo

import (
	"fmt"
	"strings"
)

// RuntimeConfigOptions controls isolated mihomo runtime config rewriting.
type RuntimeConfigOptions struct {
	ProxyPort      int
	ControllerPort int
	Group          string
	Node           string
}

// RewriteConfig rewrites mihomo config text for browsebox's isolated runtime.
func RewriteConfig(input string, opts RuntimeConfigOptions) string {
	lines := configLines(input)
	out := make([]string, 0, len(lines)+8)
	seen := map[string]bool{}

	tunSeen := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if replacement, ok := isolatedSetting(line, opts); ok {
			out = append(out, replacement)
			seen[settingKey(line)] = true
			continue
		}

		if unsafeTopLevelSetting(line) {
			continue
		}

		if settingKey(line) == "tun" {
			tunSeen = true
			block, next := rewriteTunBlock(lines, i)
			out = append(out, block...)
			i = next - 1
			continue
		}

		out = append(out, line)
	}

	appendMissingSettings(&out, seen, opts)
	if !tunSeen {
		out = append(out, "tun:", "  enable: false")
	}
	if opts.Group != "" && opts.Node != "" {
		out = append(out, "# browsebox group: "+safeCommentValue(opts.Group), "# browsebox node: "+safeCommentValue(opts.Node))
	}

	return strings.Join(out, "\n") + "\n"
}

func configLines(input string) []string {
	trimmed := strings.TrimRight(input, "\r\n")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "\n")
	for i := range parts {
		parts[i] = strings.TrimSuffix(parts[i], "\r")
	}
	return parts
}

func safeCommentValue(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < ' ' || r == 0x7f {
				b.WriteString(fmt.Sprintf(`\x%02x`, r))
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isolatedSetting(line string, opts RuntimeConfigOptions) (string, bool) {
	switch settingKey(line) {
	case "mixed-port":
		return fmt.Sprintf("mixed-port: %d", opts.ProxyPort), true
	case "allow-lan":
		return "allow-lan: false", true
	case "bind-address":
		return "bind-address: 127.0.0.1", true
	case "external-controller":
		return fmt.Sprintf("external-controller: 127.0.0.1:%d", opts.ControllerPort), true
	default:
		return "", false
	}
}

func unsafeTopLevelSetting(line string) bool {
	switch settingKey(line) {
	case "port", "socks-port", "redir-port", "tproxy-port", "external-controller-tls", "secret":
		return true
	default:
		return false
	}
}

func appendMissingSettings(out *[]string, seen map[string]bool, opts RuntimeConfigOptions) {
	if !seen["mixed-port"] {
		*out = append(*out, fmt.Sprintf("mixed-port: %d", opts.ProxyPort))
	}
	if !seen["allow-lan"] {
		*out = append(*out, "allow-lan: false")
	}
	if !seen["bind-address"] {
		*out = append(*out, "bind-address: 127.0.0.1")
	}
	if !seen["external-controller"] {
		*out = append(*out, fmt.Sprintf("external-controller: 127.0.0.1:%d", opts.ControllerPort))
	}
}

func rewriteTunBlock(lines []string, start int) ([]string, int) {
	blockIndent := leadingWhitespace(lines[start])
	key, value, ok := keyValue(lines[start])
	trimmedValue := strings.TrimSpace(value)
	if ok && key == "tun" && trimmedValue != "" && !strings.HasPrefix(trimmedValue, "#") {
		return []string{blockIndent + "tun:", blockIndent + "  enable: false"}, start + 1
	}

	end := start + 1
	for end < len(lines) && !endsBlock(lines[end], blockIndent) {
		end++
	}

	childIndent := directChildIndent(lines[start+1:end], blockIndent)
	out := make([]string, 0, end-start+1)
	out = append(out, lines[start])
	hasEnable := false
	for _, line := range lines[start+1 : end] {
		if leadingWhitespace(line) == childIndent && nestedKey(line, "enable") {
			hasEnable = true
			out = append(out, childIndent+"enable: false")
			continue
		}
		out = append(out, line)
	}
	if !hasEnable {
		out = append(out[:1], append([]string{childIndent + "enable: false"}, out[1:]...)...)
	}
	return out, end
}

func directChildIndent(lines []string, blockIndent string) string {
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			indent := leadingWhitespace(line)
			if len(indent) > len(blockIndent) {
				return indent
			}
		}
	}
	return blockIndent + "  "
}

func settingKey(line string) string {
	if leadingWhitespace(line) != "" {
		return ""
	}
	key, _, ok := keyValue(line)
	if !ok {
		return ""
	}
	return key
}

func keyValue(line string) (string, string, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	key, value, ok := strings.Cut(trimmed, ":")
	if !ok {
		return "", "", false
	}
	return strings.TrimSpace(key), value, true
}

func nestedKey(line, key string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	actual, _, ok := strings.Cut(trimmed, ":")
	return ok && strings.TrimSpace(actual) == key
}

func endsBlock(line, blockIndent string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	indent := leadingWhitespace(line)
	return len(indent) <= len(blockIndent)
}

func leadingWhitespace(line string) string {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			return line[:i]
		}
	}
	return line
}
