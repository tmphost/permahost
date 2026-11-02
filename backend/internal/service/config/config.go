// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package config loads the YAML configuration and hands typed sections to
// the services that need them.
package config

//go:generate mockgen -source=config.go -destination=mocks/config.go -package=mocks

import (
	"fmt"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type (
	// Service exposes configuration sections by their top-level key.
	Service interface {
		// Populate decodes the section under key into target. A missing key
		// leaves target untouched.
		Populate(key string, target any) error
	}

	service struct {
		sections map[string][]byte
	}
)

// _placeholder matches ${VAR} and ${VAR:default}.
var _placeholder = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::([^}]*))?\}`)

// New parses raw YAML. Placeholders are expanded after parsing, so an
// environment value can never inject YAML structure. A scalar that is a
// single placeholder is re-typed after expansion (e.g. "true" -> bool).
func New(raw []byte, getenv func(string) string) (Service, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	var root map[string]any
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}
	sections := make(map[string][]byte, len(root))
	for key, value := range root {
		// Marshalling a tree that came from yaml.Unmarshal cannot fail.
		b, _ := yaml.Marshal(expand(value, getenv))
		sections[key] = b
	}
	return &service{sections: sections}, nil
}

func (s *service) Populate(key string, target any) error {
	b, ok := s.sections[key]
	if !ok {
		return nil
	}
	if err := yaml.Unmarshal(b, target); err != nil {
		return fmt.Errorf("config: populate %q: %w", key, err)
	}
	return nil
}

func expand(value any, getenv func(string) string) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			v[k] = expand(item, getenv)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = expand(item, getenv)
		}
		return v
	case string:
		return expandString(v, getenv)
	default:
		return v
	}
}

func expandString(s string, getenv func(string) string) any {
	whole := _placeholder.FindStringIndex(s)
	out := _placeholder.ReplaceAllStringFunc(s, func(m string) string {
		parts := _placeholder.FindStringSubmatch(m)
		if env := getenv(parts[1]); env != "" {
			return env
		}
		return parts[2]
	})
	if whole == nil || whole[0] != 0 || whole[1] != len(s) {
		return out
	}
	if out == "" {
		return ""
	}
	var typed any
	if err := yaml.Unmarshal([]byte(out), &typed); err != nil {
		return out
	}
	switch typed.(type) {
	case bool, int, float64:
		return typed
	default:
		return out
	}
}
