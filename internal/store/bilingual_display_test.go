package store

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

func TestBilingualNodeLayoutPreservesIdentityAndLegacyWrites(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	first := &domain.Node{Name: "First"}
	second := &domain.Node{Name: "Second"}
	for _, n := range []*domain.Node{first, second} {
		if err := s.CreateNode(ctx, n, n.Name, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	read := func() NodeLayout {
		t.Helper()
		v, err := s.NodeLayout(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	layout := read()
	layout.Groups = []string{"stable-first", "stable-second"}
	layout.GroupNames = map[string]DisplayGroupName{"stable-first": {ZH: " 亚洲 ", EN: " Asia "}, "stable-second": {ZH: "亚洲", EN: "Asia"}}
	layout.Nodes = []NodePlacement{{ID: first.ID, Group: "stable-first"}, {ID: second.ID, Group: "stable-second"}}
	if err := s.SaveNodeLayout(ctx, layout); err != nil {
		t.Fatal("identical labels must not merge independent identities", err)
	}
	before := read()
	if before.GroupNames["stable-first"] != (DisplayGroupName{ZH: "亚洲", EN: "Asia"}) {
		t.Fatal(before)
	}
	// A label-only edit changes the CAS revision and public consistency fingerprint.
	fingerprint, err := s.ProbePublicPolicyFingerprint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	layout = read()
	layout.GroupNames["stable-first"] = DisplayGroupName{EN: "Pacific"}
	if err := s.SaveNodeLayout(ctx, layout); err != nil {
		t.Fatal(err)
	}
	after := read()
	changed, err := s.ProbePublicPolicyFingerprint(ctx)
	if err != nil || changed == fingerprint || before.Revision == after.Revision {
		t.Fatal("labels did not invalidate drafts/projection", err)
	}
	if !reflect.DeepEqual(before.Nodes, after.Nodes) || !reflect.DeepEqual(before.Groups, after.Groups) {
		t.Fatal("label edit changed placement", after)
	}
	if err := s.SaveNodeLayout(ctx, before); !errors.Is(err, ErrNodeLayoutConflict) {
		t.Fatal("stale label draft accepted", err)
	}
	// The old complete layout omits labels; both translations must survive.
	legacy := read()
	legacy.GroupNames = nil
	if err := s.SaveNodeLayout(ctx, legacy); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got, after) {
		t.Fatal("legacy layout discarded translations", got)
	}
	// A legacy key may equal another group's translated label. It is still a new identity.
	if err := s.SetMonitorGroup(ctx, second.ID, "Pacific"); err != nil {
		t.Fatal(err)
	}
	if got := read(); !reflect.DeepEqual(got.GroupNames, after.GroupNames) || got.Nodes[1].Group != "Pacific" {
		t.Fatal("legacy reassignment discarded labels", got)
	}
	node, err := s.NodeByID(ctx, first.ID)
	if err != nil || node.MonitorGroup != "stable-first" || node.MonitorGroupZH != "" || node.MonitorGroupEN != "Pacific" {
		t.Fatal("node projection", node, err)
	}
	monitor, err := s.MonitorNodes(ctx, time.Now(), time.Minute, first.ID)
	if err != nil || len(monitor) != 1 || monitor[0].Group != "stable-first" || monitor[0].GroupEN != "Pacific" {
		t.Fatal("monitor projection", monitor, err)
	}
	baseline := read()
	for _, badLabel := range []DisplayGroupName{{}, {ZH: strings.Repeat("界", 43)}, {EN: "bad\nlabel"}, {ZH: "<markup>"}, {EN: "bad\u0085label"}, {EN: string([]byte{0xff})}} {
		bad := read()
		bad.GroupNames["stable-first"] = badLabel
		if err := s.SaveNodeLayout(ctx, bad); !errors.Is(err, ErrNodeLayoutInvalid) {
			t.Fatal("invalid label accepted", badLabel, err)
		}
		if got := read(); !reflect.DeepEqual(got, baseline) {
			t.Fatal("partial write", got)
		}
	}
	bad := read()
	bad.GroupNames["unknown"] = DisplayGroupName{EN: "Unrelated"}
	if err := s.SaveNodeLayout(ctx, bad); !errors.Is(err, ErrNodeLayoutInvalid) {
		t.Fatal("unbound group metadata", err)
	}
}

func TestBilingualEntryCategoryValidationAndLegacyNameUniqueness(t *testing.T) {
	s := subDesignStore(t)
	ctx := context.Background()
	for _, c := range []*domain.EntryCategory{{NameZH: " 中文 "}, {NameEN: " English "}, {NameZH: "双语", NameEN: "Bilingual"}, {Name: "Legacy"}} {
		if err := s.CreateEntryCategory(ctx, c); err != nil {
			t.Fatal(err)
		}
		got, err := s.EntryCategoryByID(ctx, c.ID)
		if err != nil || !reflect.DeepEqual(got, c) || c.Name == "" {
			t.Fatal("roundtrip", got, c, err)
		}
	}
	for _, c := range []*domain.EntryCategory{{NameZH: "甲", NameEN: "Same"}, {NameZH: "乙", NameEN: "Same"}} {
		if err := s.CreateEntryCategory(ctx, c); err != nil {
			t.Fatal("translated labels are not identities", err)
		}
	}
	if err := s.CreateEntryCategory(ctx, &domain.EntryCategory{Name: " legacy ", NameEN: "Another"}); !errors.Is(err, ErrEntryCategoryConflict) {
		t.Fatal("legacy unique key changed", err)
	}
	for _, c := range []domain.EntryCategory{{NameEN: strings.Repeat("e", 81)}, {NameZH: strings.Repeat("类", 81)}, {NameZH: "合法", NameEN: "<script>"}, {NameZH: "非法\n值"}, {Name: "Legacy2", NameEN: string([]byte{0xff})}} {
		if err := s.CreateEntryCategory(ctx, &c); !errors.Is(err, ErrEntryCategoryInvalid) {
			t.Fatal("invalid translation", err)
		}
	}
}
