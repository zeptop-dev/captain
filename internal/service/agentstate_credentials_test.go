package service

import (
	"reflect"
	"testing"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

func TestDeliveredInboundCredentials(t *testing.T) {
	users := []spec.User{
		{ID: 1, Name: "customer-a", UUID: "identity-a", Password: "password-a", SpeedLimitMbps: 2},
		{ID: 2, Name: "customer-b", UUID: "identity-b", Password: "password-b", DeviceLimit: 3},
		{ID: 3, Name: "customer-c", UUID: "identity-c", Password: "password-c", QuotaBytes: 4096, QuotaDays: 30},
	}
	wgUser := users[1]
	wgUser.WireGuardPeers = []spec.WireGuardPeer{{UUID: "device-identity"}}
	wgUser.WireGuardLegacyDisabled = true
	for _, tc := range []struct {
		name     string
		inbounds []spec.Inbound
		want     []spec.User
	}{
		{name: "no delivered listeners", want: []spec.User{}},
		{name: "empty authorized group", inbounds: []spec.Inbound{{ScopedUsers: true}}, want: []spec.User{}},
		{name: "reverse receiver has separate identity", inbounds: []spec.Inbound{{Reverse: &spec.ReverseInbound{Receiver: true}}}, want: []spec.User{}},
		{name: "scoped union preserves parent order and limits", inbounds: []spec.Inbound{
			{ScopedUsers: true, Users: []spec.User{users[2], users[1]}},
			{ScopedUsers: true, Users: []spec.User{users[1]}},
		}, want: []spec.User{users[1], users[2]}},
		{name: "unscoped subscriber listener retains all", inbounds: []spec.Inbound{{ScopedUsers: true}, {}}, want: users},
		{name: "WG peers remain inbound scoped", inbounds: []spec.Inbound{{ScopedUsers: true, Users: []spec.User{wgUser}}}, want: []spec.User{users[1]}},
		{name: "unknown identities cannot authorize other users", inbounds: []spec.Inbound{{ScopedUsers: true, Users: []spec.User{{ID: 4}, {ID: 5}, {ID: 6}}}}, want: []spec.User{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := append([]spec.User(nil), users...)
			got := usersForDeliveredInbounds(tc.inbounds, users)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("incorrect delivered user scope: got IDs %v, want IDs %v", credentialIDs(got), credentialIDs(tc.want))
			}
			if !reflect.DeepEqual(users, before) {
				t.Fatal("filter changed the original parent records")
			}
		})
	}
}

func credentialIDs(users []spec.User) []int64 {
	ids := make([]int64, 0, len(users))
	for _, u := range users {
		ids = append(ids, u.ID)
	}
	return ids
}
