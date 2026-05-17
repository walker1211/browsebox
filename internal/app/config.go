package app

import (
	"fmt"
	"os"
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
	for lineNumber, rawLine := range strings.Split(string(content), "\n") {
		line := stripConfigComment(rawLine)
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.TrimLeft(line, " \t") == line && strings.HasSuffix(trimmed, ":") {
			section = strings.TrimSuffix(trimmed, ":")
			continue
		}
		if section != "nodes" {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return fmt.Errorf("line %d: expected key: value", lineNumber+1)
		}
		intValue, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || intValue <= 0 {
			return fmt.Errorf("line %d: %s must be a positive integer", lineNumber+1, strings.TrimSpace(key))
		}
		switch strings.TrimSpace(key) {
		case "concurrency":
			opts.NodesConcurrency = intValue
		case "delay_timeout_ms", "delay-timeout-ms":
			opts.DelayTimeoutMS = intValue
		}
	}
	return nil
}

func stripConfigComment(line string) string {
	if before, _, ok := strings.Cut(line, "#"); ok {
		return before
	}
	return line
}
