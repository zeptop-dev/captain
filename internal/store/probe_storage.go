package store

import (
	"errors"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/zeptop-dev/bosun/pkg/spec"
)

// Storage choices are private: mount paths must never be copied to public Info.
// Their order is the public order; the first two entries appear on home cards.
type NodeStorage struct {
	Mode  string            `json:"mode"`
	Items []NodeStorageItem `json:"items"`
}

type NodeStorageItem struct {
	Mount string `json:"mount"`
	Name  string `json:"name"`
}

var ErrNodeStorage = errors.New("invalid storage display settings")

func (s *NodeStorage) normalize() {
	if s.Mode == "" {
		s.Mode = "auto"
	}
	if s.Items == nil {
		s.Items = []NodeStorageItem{}
	}
}

func validStorageMount(m string) bool {
	return len(m) <= 1024 && strings.HasPrefix(m, "/") && path.Clean(m) == m && spec.Plain(m)
}

func (s *NodeStorage) Validate() error {
	s.normalize()
	if s.Mode != "auto" && s.Mode != "manual" || len(s.Items) > 32 {
		return ErrNodeStorage
	}
	seen := map[string]bool{}
	for i := range s.Items {
		item := &s.Items[i]
		item.Name = strings.TrimSpace(item.Name)
		if !validStorageMount(item.Mount) || seen[item.Mount] || !utf8.ValidString(item.Name) || utf8.RuneCountInString(item.Name) > 32 || (item.Name != "" && !spec.PublicLabel(item.Name)) {
			return ErrNodeStorage
		}
		seen[item.Mount] = true
	}
	if s.Mode == "auto" {
		s.Items = []NodeStorageItem{}
	}
	return nil
}

// RootVisible also controls legacy root-only summaries and their history.
// Selecting only a data volume must not leave root capacity in the old API.
func (s NodeStorage) RootVisible() bool {
	return s.Mode != "manual" || slices.ContainsFunc(s.Items, func(i NodeStorageItem) bool { return i.Mount == "/" })
}

type StorageCandidate struct {
	Mount       string  `json:"mount"`
	Device      string  `json:"device"`
	Type        string  `json:"type"`
	Kind        string  `json:"kind"`
	Total       *uint64 `json:"total"`
	Used        *uint64 `json:"used"`
	Recommended bool    `json:"recommended"`
	members     []string
}

// A filesystem's timestamp is relative to the agent's resource clock. Receipt
// age is added separately so clock skew and ordinary status requests cannot
// refresh an old asynchronous measurement.
func filesystemFresh(f spec.FilesystemResource, resourceAt int64, received, now time.Time) bool {
	if received.IsZero() || now.Sub(received) > 90*time.Second {
		return false
	}
	if f.SampledAt == 0 {
		return f.ID == ""
	} // legacy synchronous collectors
	if resourceAt <= 0 || f.SampledAt <= 0 {
		return false
	}
	delta := resourceAt - f.SampledAt
	if delta < -1000 || delta > 90000 {
		return false
	}
	age := time.Duration(delta)*time.Millisecond + max(time.Duration(0), now.Sub(received))
	return age <= 90*time.Second
}

func freshFilesystem(f spec.FilesystemResource, r *spec.Resources, previous int64) bool {
	if f.ID == "" && f.SampledAt == 0 {
		return true
	} // old synchronous reports
	if f.SampledAt <= previous || f.SampledAt <= 0 || r == nil {
		return false
	}
	if f.Total == nil || f.Used == nil || *f.Total == 0 || *f.Used > *f.Total {
		return false
	}
	at := time.UnixMilli(r.At)
	return filesystemFresh(f, r.At, at, at)
}

// StorageCandidates describes filesystems, not physical disks. Never sum their
// capacities: pools, subvolumes and aliases can share backing storage.
func StorageCandidates(host *spec.SystemStatus, received, now time.Time) []StorageCandidate {
	out := []StorageCandidate{}
	seenMount, seenID := map[string]bool{}, map[string]bool{}
	if host != nil && host.Resources != nil {
		for _, f := range host.Resources.Filesystems {
			if len(out) >= 128 {
				break
			}
			if !validStorageMount(f.Mount) || seenMount[f.Mount] || (f.ID != "" && seenID[f.ID]) {
				continue
			}
			seenMount[f.Mount] = true
			if f.ID != "" {
				seenID[f.ID] = true
			}
			c := StorageCandidate{Mount: f.Mount, Device: f.Device, Type: f.Type, Kind: "volume", members: f.MemberMounts}
			if f.Pool || f.Type == "fuse.mergerfs" {
				c.Kind = "pool"
			}
			if f.Mount == "/" {
				c.Kind = "system"
			}
			if filesystemFresh(f, host.Resources.At, received, now) && f.Total != nil && f.Used != nil && *f.Total > 0 && *f.Used <= *f.Total {
				total, used := *f.Total, *f.Used
				c.Total, c.Used = &total, &used
			}
			out = append(out, c)
		}
	}
	if !seenMount["/"] {
		c := StorageCandidate{Mount: "/", Kind: "system"}
		if host != nil && !received.IsZero() && now.Sub(received) <= 90*time.Second && host.Validity().Disk && host.DiskTotal > 0 && host.DiskUsed <= host.DiskTotal {
			total, used := host.DiskTotal, host.DiskUsed
			c.Total, c.Used = &total, &used
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Mount == "/") != (b.Mount == "/") {
			return a.Mount == "/"
		}
		if (a.Kind == "pool") != (b.Kind == "pool") {
			return a.Kind == "pool"
		}
		if (a.Total != nil) != (b.Total != nil) {
			return a.Total != nil
		}
		if a.Total != nil && b.Total != nil && *a.Total != *b.Total {
			return *a.Total > *b.Total
		}
		return a.Mount < b.Mount
	})
	members := map[string]bool{}
	for _, c := range out {
		for _, mount := range c.members {
			members[mount] = true
		}
	}
	chosen := 0
	for i := range out {
		c := &out[i]
		if c.Mount == "/" || (chosen < 2 && !members[c.Mount] && !bootMount(c.Mount)) {
			c.Recommended = true
			chosen++
		}
	}
	return out
}

func bootMount(m string) bool {
	return m == "/boot" || strings.HasPrefix(m, "/boot/") || m == "/efi" || strings.HasPrefix(m, "/efi/")
}

// PublicStorage deliberately has no mount, device, filesystem type, member
// paths or collector ID. Names are operator-chosen public labels, or empty for
// the frontend's localized defaults. IDs are display positions only.
type PublicStorage struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Kind  string  `json:"kind"`
	Total *uint64 `json:"total"`
	Used  *uint64 `json:"used"`
}

func (s NodeStorage) Public(candidates []StorageCandidate) []PublicStorage {
	items := s.Items
	if s.Mode != "manual" {
		items = nil
		for _, c := range candidates {
			if c.Recommended {
				items = append(items, NodeStorageItem{Mount: c.Mount})
			}
		}
	}
	out := make([]PublicStorage, 0, len(items))
	for i, item := range items {
		v := PublicStorage{ID: "storage-" + strconv.Itoa(i+1), Name: item.Name, Kind: "volume"}
		if item.Mount == "/" {
			v.Kind = "system"
		}
		for _, c := range candidates {
			if c.Mount == item.Mount {
				v.Kind, v.Total, v.Used = c.Kind, c.Total, c.Used
				break
			}
		}
		out = append(out, v)
	}
	return out
}
