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
	listKey := ""
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := stripConfigComment(rawLine)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if isTopLevelConfigKey(line) && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			listKey = ""
			continue
		}
		if section == "" {
			continue
		}
		if item, ok := strings.CutPrefix(trimmed, "- "); ok {
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
		if value == "" {
			listKey = key
			if section == "session" && (key == "health_urls" || key == "health-urls") {
				opts.HealthURLs = nil
			}
			continue
		}
		listKey = ""
		if err := applyConfigValue(section, key, value, opts); err != nil {
			return fmt.Errorf("line %d: %w", lineNumber+1, err)
		}
	}
	return nil
}

func isTopLevelConfigKey(line string) bool {
	return strings.TrimLeft(line, " \t") == line
}

func applyConfigValue(section, key, value string, opts *Options) error {
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
		return applyNodesConfig(key, value, opts)
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
			opts.HealthURLs = append(opts.HealthURLs, cleanConfigString(value))
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
		opts.HealthURLs = []string{cleanConfigString(value)}
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
	}

	intValue, ok, err := parsePositiveConfigInt(key, value)
	if err != nil || !ok {
		return err
	}
	switch key {
	case "concurrency":
		opts.NodesConcurrency = intValue
	case "delay_timeout_ms", "delay-timeout-ms":
		opts.DelayTimeoutMS = intValue
	}
	return nil
}

func parsePositiveConfigInt(key, value string) (int, bool, error) {
	switch key {
	case "proxy", "proxy_port", "proxy-port", "controller", "controller_port", "controller-port", "devtools", "devtools_port", "devtools-port", "concurrency", "delay_timeout_ms", "delay-timeout-ms":
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
