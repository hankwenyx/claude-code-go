package config

import (
	"path/filepath"
	"strings"
)

// PermissionRules holds the parsed permission rules
type PermissionRules struct {
	Allow []string
	Deny  []string
	Ask   []string
}

// ParsePermissionRules extracts PermissionRules from PermissionsSettings
func ParsePermissionRules(p PermissionsSettings) PermissionRules {
	return PermissionRules{
		Allow: p.Allow,
		Deny:  p.Deny,
		Ask:   p.Ask,
	}
}

// MatchRule checks whether a rule string matches the given tool name and input argument.
// Rule formats:
//   - "Bash"            → matches any Bash call
//   - "Bash(git *)"     → matches Bash with command matching "git *"
//   - "FileEdit(src/**)" → matches FileEdit with path matching "src/**"
func MatchRule(rule, toolName, arg string) bool {
	// Rule with no parens: match tool by name
	openParen := strings.Index(rule, "(")
	if openParen == -1 {
		return strings.EqualFold(rule, toolName)
	}

	// Rule with parens: tool name + pattern
	ruleToolName := rule[:openParen]
	if !strings.EqualFold(ruleToolName, toolName) {
		return false
	}

	closeParen := strings.LastIndex(rule, ")")
	if closeParen == -1 {
		return false
	}
	pattern := rule[openParen+1 : closeParen]

	// Prefix syntax "npm run:*"
	if idx := strings.Index(pattern, ":*"); idx != -1 {
		prefix := pattern[:idx]
		return strings.HasPrefix(arg, prefix)
	}

	// For Bash commands, use glob matching that allows "/" in *
	// (filepath.Match treats * as not matching /)
	if strings.EqualFold(toolName, "bash") {
		return globMatch(pattern, arg)
	}

	// For file tools, use filepath.Match (respects path separators)
	matched, err := filepath.Match(pattern, arg)
	if err != nil {
		return strings.HasPrefix(arg, pattern)
	}
	return matched
}

// globMatch is a simple glob matcher where * matches any character including /
func globMatch(pattern, s string) bool {
	// Empty pattern
	if pattern == "" {
		return s == ""
	}

	// Find first *
	star := strings.Index(pattern, "*")
	if star == -1 {
		return pattern == s
	}

	// Match prefix before *
	prefix := pattern[:star]
	if !strings.HasPrefix(s, prefix) {
		return false
	}

	// Try matching the rest after * against all suffixes of s[len(prefix):]
	rest := pattern[star+1:]
	remaining := s[len(prefix):]

	if rest == "" {
		return true // * matches everything remaining
	}

	// Try matching rest against each suffix position
	for i := 0; i <= len(remaining); i++ {
		if globMatch(rest, remaining[i:]) {
			return true
		}
	}
	return false
}
