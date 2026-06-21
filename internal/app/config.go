package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const DefaultConfigPath = "configs/config.yaml"

func LoadConfigFile(path string, opts *Options) error {
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read browsebox config %s: %w", path, err)
	}
	if err := applyConfig(content, opts); err != nil {
		return fmt.Errorf("parse browsebox config %s: %w", path, err)
	}
	return nil
}

func applyConfig(content []byte, opts *Options) error {
	section := ""
	subsection := ""
	listKey := ""
	capabilityCheckIndex := -1
	capabilityCheckItemIndent := -1
	capabilityCheckListKey := ""
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := stripConfigComment(rawLine)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isTopLevelConfigKey(line) && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			subsection = ""
			listKey = ""
			capabilityCheckIndex = -1
			capabilityCheckItemIndent = -1
			capabilityCheckListKey = ""
			continue
		}
		if section == "" {
			continue
		}
		indent := configIndent(line)
		if section == "nodes" && listKey == "capability_checks" && capabilityCheckIndex >= 0 && indent > capabilityCheckItemIndent {
			if item, ok := strings.CutPrefix(trimmed, "- "); ok {
				if err := applyCapabilityCheckListItem(&opts.NodesCapabilityChecks[capabilityCheckIndex], capabilityCheckListKey, strings.TrimSpace(item)); err != nil {
					return fmt.Errorf("line %d: %w", lineNumber+1, err)
				}
				continue
			}
			key, value, ok := strings.Cut(trimmed, ":")
			if !ok {
				return fmt.Errorf("line %d: expected key: value", lineNumber+1)
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if value == "" {
				capabilityCheckListKey = key
				continue
			}
			capabilityCheckListKey = ""
			if err := applyCapabilityCheckValue(&opts.NodesCapabilityChecks[capabilityCheckIndex], key, value); err != nil {
				return fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			continue
		}
		if item, ok := strings.CutPrefix(trimmed, "- "); ok {
			if section == "nodes" && listKey == "capability_checks" {
				check, err := newCapabilityCheckFromListItem(strings.TrimSpace(item))
				if err != nil {
					return fmt.Errorf("line %d: %w", lineNumber+1, err)
				}
				opts.NodesCapabilityChecks = append(opts.NodesCapabilityChecks, check)
				capabilityCheckIndex = len(opts.NodesCapabilityChecks) - 1
				capabilityCheckItemIndent = indent
				capabilityCheckListKey = ""
				continue
			}
			if err := applyConfigListItem(section, listKey, strings.TrimSpace(item), opts); err != nil {
				return fmt.Errorf("line %d: %w", lineNumber+1, err)
			}
			continue
		}

		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return fmt.Errorf("line %d: expected key: value", lineNumber+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if section == "nodes" && indent == 2 && value == "" && (key == "health" || key == "capability") {
			subsection = key
			listKey = ""
			capabilityCheckIndex = -1
			capabilityCheckItemIndent = -1
			capabilityCheckListKey = ""
			continue
		}
		if section == "nodes" && indent <= 2 {
			subsection = ""
		}
		if value == "" {
			listKey = configListKey(section, subsection, key)
			capabilityCheckIndex = -1
			capabilityCheckItemIndent = -1
			capabilityCheckListKey = ""
			switch {
			case section == "session" && listKey == "health_urls":
				opts.HealthURLs = nil
				opts.SessionHealthURLs = nil
			case section == "nodes" && listKey == "health_urls":
				opts.NodesHealthURLs = nil
			case section == "nodes" && listKey == "capability_checks":
				opts.NodesCapabilityChecks = nil
			}
			continue
		}
		listKey = ""
		capabilityCheckIndex = -1
		capabilityCheckItemIndent = -1
		capabilityCheckListKey = ""
		if err := applyConfigValue(section, subsection, key, value, opts); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
	}
	return nil
}

func isTopLevelConfigKey(line string) bool {
	return strings.TrimLeft(line, " \t") == line
}

func configIndent(line string) int {
	return len(line) - len(strings.TrimLeft(line, " \t"))
}

func configListKey(section, subsection, key string) string {
	switch section {
	case "session":
		if key == "health_urls" || key == "health-urls" {
			return "health_urls"
		}
	case "nodes":
		switch {
		case subsection == "health" && (key == "urls" || key == "health_urls" || key == "health-urls"):
			return "health_urls"
		case subsection == "capability" && (key == "checks" || key == "capability_checks" || key == "capability-checks"):
			return "capability_checks"
		case key == "health_urls" || key == "health-urls":
			return "health_urls"
		case key == "capability_checks" || key == "capability-checks":
			return "capability_checks"
		}
	}
	return key
}

func newCapabilityCheckFromListItem(item string) (CapabilityCheck, error) {
	var check CapabilityCheck
	key, value, ok := strings.Cut(item, ":")
	if !ok {
		return CapabilityCheck{}, fmt.Errorf("capability check item must start with name: value")
	}
	if err := applyCapabilityCheckValue(&check, strings.TrimSpace(key), strings.TrimSpace(value)); err != nil {
		return CapabilityCheck{}, err
	}
	return check, nil
}

func applyCapabilityCheckValue(check *CapabilityCheck, key, value string) error {
	switch key {
	case "name":
		check.Name = cleanConfigString(value)
	case "url":
		check.URL = cleanConfigString(value)
	default:
		return fmt.Errorf("unknown capability check key %q", key)
	}
	return nil
}

func applyCapabilityCheckListItem(check *CapabilityCheck, key, value string) error {
	switch key {
	case "pass_status", "pass-status":
		status, err := parseCapabilityStatus(value, key)
		if err != nil {
			return err
		}
		check.PassStatus = append(check.PassStatus, status)
	case "fail_status", "fail-status":
		status, err := parseCapabilityStatus(value, key)
		if err != nil {
			return err
		}
		check.FailStatus = append(check.FailStatus, status)
	case "fail_body_contains", "fail-body-contains":
		check.FailBodyContains = append(check.FailBodyContains, cleanConfigString(value))
	default:
		return fmt.Errorf("unknown capability check list key %q", key)
	}
	return nil
}

func parseCapabilityStatus(value, key string) (int, error) {
	status, err := strconv.Atoi(cleanConfigString(value))
	if err != nil || status < 100 || status > 599 {
		return 0, fmt.Errorf("%s must contain HTTP status codes", key)
	}
	return status, nil
}

func applyConfigValue(section, subsection, key, value string, opts *Options) error {
	switch section {
	case "mihomo":
		return applyMihomoConfig(key, value, opts)
	case "browser":
		return applyBrowserConfig(key, value, opts)
	case "runtime":
		return applyRuntimeConfig(key, value, opts)
	case "ports":
		return applyPortsConfig(key, value, opts)
	case "session":
		return applySessionConfig(key, value, opts)
	case "nodes":
		switch subsection {
		case "health":
			return applyNodesHealthConfig(key, value, opts)
		case "capability":
			return applyNodesCapabilityConfig(key, value, opts)
		default:
			return applyNodesConfig(key, value, opts)
		}
	default:
		return nil
	}
}

func applyConfigListItem(section, key, value string, opts *Options) error {
	switch section {
	case "browser":
		if key == "chrome_args" || key == "chrome-args" {
			appendChromeArg(opts, cleanConfigString(value))
		}
	case "session":
		if key == "health_urls" || key == "health-urls" {
			cleaned := cleanConfigString(value)
			opts.HealthURLs = append(opts.HealthURLs, cleaned)
			opts.SessionHealthURLs = append(opts.SessionHealthURLs, cleaned)
		}
	case "nodes":
		if key == "health_urls" || key == "health-urls" {
			opts.NodesHealthURLs = append(opts.NodesHealthURLs, cleanConfigString(value))
		}
	}
	return nil
}

func applyMihomoConfig(key, value string, opts *Options) error {
	switch key {
	case "controller_socket", "controller-socket":
		opts.ControllerSocket = expandConfigPath(value)
	case "controller_url", "controller-url":
		opts.ControllerURL = cleanConfigString(value)
	case "controller_pipe", "controller-pipe":
		opts.ControllerPipe = cleanConfigString(value)
	case "config_path", "config-path", "config":
		opts.SourceConfigPath = expandConfigPath(value)
	case "binary_path", "binary-path", "binary":
		opts.MihomoBinaryPath = expandConfigPath(value)
	case "interface_name", "interface-name":
		opts.MihomoInterfaceName = cleanConfigString(value)
	}
	return nil
}

func applyBrowserConfig(key, value string, opts *Options) error {
	switch key {
	case "chrome_path", "chrome-path", "chrome":
		opts.ChromeBinaryPath = expandConfigPath(value)
	case "profile_dir", "profile-dir":
		opts.ChromeProfileDir = expandConfigPath(value)
	case "chrome_args", "chrome-args":
		if cleanConfigString(value) == "[]" {
			opts.ChromeArgs = []string{}
		} else {
			appendChromeArg(opts, cleanConfigString(value))
		}
	case "headless":
		headless, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("headless must be true or false")
		}
		opts.BrowserHeadless = headless
	}
	return nil
}

func appendChromeArg(opts *Options, value string) {
	name, _, _ := strings.Cut(strings.TrimLeft(strings.TrimSpace(value), "-"), "=")
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	for _, existing := range opts.ChromeArgs {
		existingName, _, _ := strings.Cut(strings.TrimLeft(strings.TrimSpace(existing), "-"), "=")
		if strings.TrimSpace(existingName) == name {
			return
		}
	}
	opts.ChromeArgs = append(opts.ChromeArgs, value)
}

func applyRuntimeConfig(key, value string, opts *Options) error {
	switch key {
	case "dir":
		opts.RuntimeDir = expandConfigPath(value)
	case "cache_dir", "cache-dir":
		opts.RuntimeCacheDir = expandConfigPath(value)
	case "state_dir", "state-dir":
		opts.StateDir = expandConfigPath(value)
	case "keep":
		keep, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("keep must be true or false")
		}
		opts.Keep = keep
	}
	return nil
}

func applyPortsConfig(key, value string, opts *Options) error {
	port, ok, err := parsePositiveConfigInt(key, value)
	if err != nil || !ok {
		return err
	}
	switch key {
	case "proxy", "proxy_port", "proxy-port":
		opts.ProxyPort = port
	case "controller", "controller_port", "controller-port":
		opts.ControllerPort = port
	case "devtools", "devtools_port", "devtools-port":
		opts.DevToolsPort = port
	}
	return nil
}

func applySessionConfig(key, value string, opts *Options) error {
	switch key {
	case "group":
		opts.Group = cleanConfigString(value)
	case "node":
		opts.DefaultNode = cleanConfigString(value)
	case "url", "target_url", "target-url":
		opts.TargetURL = cleanConfigString(value)
	case "health_url", "health-url":
		cleaned := cleanConfigString(value)
		opts.HealthURLs = []string{cleaned}
		opts.SessionHealthURLs = []string{cleaned}
	case "health_urls", "health-urls":
		if cleanConfigString(value) == "[]" {
			opts.HealthURLs = []string{}
			opts.SessionHealthURLs = []string{}
		}
	case "select_fastest", "select-fastest":
		selectFastest, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		opts.SessionSelectFastest = selectFastest
	}
	return nil
}

func applyNodesHealthConfig(key, value string, opts *Options) error {
	switch key {
	case "url", "health_url", "health-url":
		opts.NodesHealthURLs = []string{cleanConfigString(value)}
		return nil
	case "urls", "health_urls", "health-urls":
		if cleanConfigString(value) == "[]" {
			opts.NodesHealthURLs = []string{}
		}
		return nil
	}

	if key == "probe_interval_ms" || key == "probe-interval-ms" {
		intervalMS, err := strconv.Atoi(cleanConfigString(value))
		if err != nil || intervalMS < 0 {
			return fmt.Errorf("%s must be zero or a positive integer", key)
		}
		opts.NodeProbeIntervalMS = intervalMS
		return nil
	}

	intValue, ok, err := parsePositiveConfigInt(key, value)
	if err != nil || !ok {
		return err
	}
	switch key {
	case "concurrency":
		opts.NodesConcurrency = intValue
	case "probe_rounds", "probe-rounds":
		opts.NodeProbeRounds = intValue
	}
	return nil
}

func applyNodesCapabilityConfig(key, value string, opts *Options) error {
	switch key {
	case "checks", "capability_checks", "capability-checks":
		if cleanConfigString(value) == "[]" {
			opts.NodesCapabilityChecks = []CapabilityCheck{}
		}
		return nil
	case "concurrency":
		concurrency, err := strconv.Atoi(cleanConfigString(value))
		if err != nil || concurrency <= 0 {
			return fmt.Errorf("%s must be a positive integer", key)
		}
		opts.NodesCapabilityConcurrency = concurrency
		return nil
	}
	return nil
}

func applyNodesConfig(key, value string, opts *Options) error {
	switch key {
	case "show_unhealthy", "show-unhealthy":
		showUnhealthy, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		opts.ShowUnhealthyNodes = showUnhealthy
		return nil
	case "highlight_current", "highlight-current":
		highlightCurrent, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		opts.HighlightCurrentNode = highlightCurrent
		return nil
	case "select_fastest", "select-fastest":
		selectFastest, err := strconv.ParseBool(cleanConfigString(value))
		if err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
		opts.NodesSelectFastest = selectFastest
		return nil
	case "health_url", "health-url":
		opts.NodesHealthURLs = []string{cleanConfigString(value)}
		return nil
	case "health_urls", "health-urls":
		if cleanConfigString(value) == "[]" {
			opts.NodesHealthURLs = []string{}
		}
		return nil
	case "capability_checks", "capability-checks":
		if cleanConfigString(value) == "[]" {
			opts.NodesCapabilityChecks = []CapabilityCheck{}
		}
		return nil
	}

	if key == "probe_interval_ms" || key == "probe-interval-ms" {
		intervalMS, err := strconv.Atoi(cleanConfigString(value))
		if err != nil || intervalMS < 0 {
			return fmt.Errorf("%s must be zero or a positive integer", key)
		}
		opts.NodeProbeIntervalMS = intervalMS
		return nil
	}

	intValue, ok, err := parsePositiveConfigInt(key, value)
	if err != nil || !ok {
		return err
	}
	switch key {
	case "concurrency":
		opts.NodesConcurrency = intValue
	case "probe_rounds", "probe-rounds":
		opts.NodeProbeRounds = intValue
	case "delay_timeout_ms", "delay-timeout-ms":
		opts.DelayTimeoutMS = intValue
	}
	return nil
}

func parsePositiveConfigInt(key, value string) (int, bool, error) {
	switch key {
	case "proxy", "proxy_port", "proxy-port", "controller", "controller_port", "controller-port", "devtools", "devtools_port", "devtools-port", "concurrency", "probe_rounds", "probe-rounds", "delay_timeout_ms", "delay-timeout-ms":
		intValue, err := strconv.Atoi(cleanConfigString(value))
		if err != nil || intValue <= 0 {
			return 0, true, fmt.Errorf("%s must be a positive integer", key)
		}
		return intValue, true, nil
	default:
		return 0, false, nil
	}
}

func expandConfigPath(value string) string {
	path := cleanConfigString(value)
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home
		}
	}
	if rest, ok := strings.CutPrefix(path, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, rest)
		}
	}
	return path
}

func cleanConfigString(value string) string {
	cleaned := strings.TrimSpace(value)
	if len(cleaned) >= 2 {
		if (cleaned[0] == '"' && cleaned[len(cleaned)-1] == '"') || (cleaned[0] == '\'' && cleaned[len(cleaned)-1] == '\'') {
			return cleaned[1 : len(cleaned)-1]
		}
	}
	return cleaned
}

func stripConfigComment(line string) string {
	if before, _, ok := strings.Cut(line, "#"); ok {
		return before
	}
	return line
}
