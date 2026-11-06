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
