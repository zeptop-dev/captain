package store

import "testing"

func TestNodeDomainSuggestionScopes(t *testing.T) {
	const path = "/api/admin/nodes/domain-suggestion"
	for _, tc := range []struct {
		policy string
		want   bool
	}{
		{`full|["nodes:*"]`, false},
		{`full|["domains:write"]`, false},
		{`full|["domains:read"]`, true},
		{`read|["domains:read"]`, true},
		{`full|["POST /api/admin/nodes/domain-suggestion"]`, true},
	} {
		if got := TokenAllowsRequest(tc.policy, "POST", path, "POST "+path); got != tc.want {
			t.Errorf("%s: %v", tc.policy, got)
		}
	}
	if TokenAllowsRequest(`read|["domains:*"]`, "POST", "/api/admin/domains", "POST /api/admin/domains") {
		t.Fatal("read token gained domain mutation")
	}
}

func TestIngressDomainSuggestionScopes(t *testing.T) {
	const path = "/api/admin/ingresses/domain-suggestion"
	for _, tc := range []struct {
		policy string
		want   bool
	}{
		{`full|["ingresses:*"]`, false},
		{`full|["nodes:*"]`, false},
		{`full|["domains:write"]`, false},
		{`full|["domains:read"]`, true},
		{`read|["domains:read"]`, true},
		{`full|["POST /api/admin/ingresses/domain-suggestion"]`, true},
		{`full|["POST /api/admin/nodes/domain-suggestion"]`, false},
	} {
		if got := TokenAllowsRequest(tc.policy, "POST", path, "POST "+path); got != tc.want {
			t.Errorf("%s: %v", tc.policy, got)
		}
	}
	if TokenAllowsRequest(`read|["ingresses:*"]`, "POST", "/api/admin/nodes/1/ingresses", "POST /api/admin/nodes/{id}/ingresses") {
		t.Fatal("read token gained ingress mutation")
	}
}
