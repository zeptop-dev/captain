package store

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// These identifiers are also the first resource segment of the admin API.
// Scopes only narrow an employee's existing role; they cannot grant a role.
var TokenResources = []string{"admin-log", "articles", "audit-log", "audit-rules", "certificates", "coupons", "config-presets", "dashboard", "domains", "entries", "entry-categories", "external", "gifts", "groups", "inbounds", "infrastructure", "ingresses", "keys", "line-changes", "me", "metrics", "monitoring", "node-access", "node-dns-sync", "node-service-control", "nodes", "orders", "ping-tasks", "plans", "renewals", "settings", "speedtest", "system", "tickets", "tokens", "users", "withdrawals"}

var endpointGrant = regexp.MustCompile(`^(GET|POST|PUT|PATCH|DELETE) /api/admin/[A-Za-z0-9_{}/-]+$`)

// nil means the pre-existing role + full/read token. [] is a restricted token
// with no resource access. Never normalize an empty restriction into full.
func ValidateTokenScopes(scopes []string) error {
	if len(scopes) > 100 {
		return fmt.Errorf("at most 100 token permissions are allowed")
	}
	for _, s := range scopes {
		if len(s) > 200 {
			return fmt.Errorf("token permission is too long")
		}
		if s == "*" || endpointGrant.MatchString(s) {
			continue
		}
		resource, action, ok := strings.Cut(s, ":")
		if !ok || !slices.Contains(TokenResources, resource) || (action != "read" && action != "write" && action != "*") {
			return fmt.Errorf("invalid token permission: %s", s)
		}
	}
	return nil
}

func tokenPolicy(base string, scopes []string) string {
	if scopes == nil {
		return base
	}
	raw, _ := json.Marshal(scopes)
	return base + "|" + string(raw)
}

// TokenAllowsRequest is shared by REST and MCP, whose tools declare the same
// route contracts. Endpoint grants use router patterns, never actual IDs.
func TokenAllowsRequest(policy, method, path, pattern string) bool {
	base, raw, restricted := strings.Cut(policy, "|")
	if method == "HEAD" {
		method = "GET"
	}
	if base != "full" && base != ScopeRead {
		return false
	}
	if base == ScopeRead && method != "GET" {
		return false
	}
	if !restricted {
		return true
	}
	var scopes []string
	if json.Unmarshal([]byte(raw), &scopes) != nil || scopes == nil || ValidateTokenScopes(scopes) != nil {
		return false
	}
	resource := strings.Split(strings.TrimPrefix(path, "/api/admin/"), "/")[0]
	if resource == "nodes" && strings.HasSuffix(path, "/dns-sync") {
		resource = "node-dns-sync"
	}
	if resource == "nodes" && strings.HasSuffix(path, "/service-control") {
		resource = "node-service-control"
	}
	// A narrowed token cannot mint an unrestricted successor or enroll login
	// credentials. Interactive sessions retain their existing management APIs.
	if method != "GET" && (resource == "tokens" || resource == "2fa" || resource == "passkeys" || resource == "admins") {
		return false
	}
	action := "write"
	if method == "GET" {
		action = "read"
	}
	for _, s := range scopes {
		if s == "*" || s == resource+":*" || s == resource+":"+action || (pattern != "" && s == pattern) {
			return true
		}
	}
	return false
}
