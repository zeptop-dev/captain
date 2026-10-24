package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/zeptop-dev/bosun/pkg/spec"
	"github.com/zeptop-dev/bosun/pkg/subdesign"
	"github.com/zeptop-dev/captain/internal/store"
	"github.com/zeptop-dev/captain/internal/subpreset"
)

var errSubDesignTooLarge = errors.New("subscription design exceeds 256 KiB")

type subLibraryInfo struct {
	Revision string `json:"revision"`
	SyncedAt int64  `json:"synced_at"`
	Count    int    `json:"count"`
}

type subPresetInfo struct {
	Key           string `json:"key"`
	Name          string `json:"name"`
	Source        string `json:"source"`
	Revision      string `json:"revision,omitempty"`
	URL           string `json:"url,omitempty"`
	Supported     bool   `json:"supported"`
	Reason        string `json:"reason,omitempty"`
	OfficialNotes string `json:"official_notes,omitempty"`
}

func presetInfo(p store.SubDesignPreset) subPresetInfo {
	info := subPresetInfo{Key: p.Key, Name: p.Name, Source: p.Source, Revision: p.Revision, URL: p.URL, Supported: p.Supported, Reason: p.Reason}
	if p.Source == "acl4ssr" {
		info.OfficialNotes = subpreset.OfficialNotes(p.Content)
	}
	return info
}

func (h *handlers) subPresetCatalogue(ctx context.Context) (subLibraryInfo, []subPresetInfo, error) {
	var library store.SubPresetLibrary
	if err := h.Store.GetSetting(ctx, store.SettingSubPresetLibrary, &library); err != nil {
		return subLibraryInfo{}, nil, err
	}
	var custom []store.SubDesignPreset
	if err := h.Store.GetSetting(ctx, store.SettingSubCustomPresets, &custom); err != nil {
		return subLibraryInfo{}, nil, err
	}
	presets := []subPresetInfo{{Key: "basic", Name: "基础：国内直连，其余代理", Source: "builtin", Supported: true}}
	for _, p := range library.Items {
		presets = append(presets, presetInfo(p))
	}
	for _, p := range custom {
		presets = append(presets, presetInfo(p))
	}
	return subLibraryInfo{Revision: library.Revision, SyncedAt: library.SyncedAt, Count: len(library.Items)}, presets, nil
}

func (h *handlers) subDesignPreset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	// Keep the old preset routes working for existing API clients. Only basic
	// is listed: the old hand-written ACL examples are not official imports.
	if d, found := subdesign.Preset(key); found {
		d.Source = &subdesign.PresetSource{Key: key, Name: key}
		ok(w, d)
		return
	}
	var items []store.SubDesignPreset
	if strings.HasPrefix(key, "custom:") {
		if err := h.Store.GetSetting(r.Context(), store.SettingSubCustomPresets, &items); err != nil {
			serverErr(w, err)
			return
		}
	} else if strings.HasPrefix(key, "acl4ssr:") {
		var library store.SubPresetLibrary
		if err := h.Store.GetSetting(r.Context(), store.SettingSubPresetLibrary, &library); err != nil {
			serverErr(w, err)
			return
		}
		items = library.Items
	}
	for _, p := range items {
		if p.Key != key {
			continue
		}
		if !p.Supported || p.Design == nil {
			fail(w, 422, p.Reason)
			return
		}
		ok(w, p.Design)
		return
	}
	fail(w, 404, "unknown preset")
}

// This context flag is internal to the two-phase sync operation. Its first
// authorization phase does not audit a successful write that hasn't happened.
// The second phase uses the original request and audits success/failure normally.
type subPresetAuthOnlyKey struct{}

func (h *handlers) syncSubDesignPresets(w http.ResponseWriter, r *http.Request) {
	authorized := false
	h.Store.Accounts.RLock()
	h.requireAdmin(func(_ http.ResponseWriter, _ *http.Request) { authorized = true })(w, r.WithContext(context.WithValue(r.Context(), subPresetAuthOnlyKey{}, true)))
	h.Store.Accounts.RUnlock()
	if !authorized {
		return
	}
	finish := func(next http.HandlerFunc) {
		h.Store.Accounts.RLock()
		defer h.Store.Accounts.RUnlock()
		// A reset, revoked session/token or changed staff role while fetching
		// cannot authorize a write to the new/current account state.
		h.requireAdmin(next)(w, r)
	}
	if !h.subPresetSync.TryLock() {
		finish(func(w http.ResponseWriter, r *http.Request) {
			fail(w, 409, "official preset synchronization is already running")
		})
		return
	}
	defer h.subPresetSync.Unlock()
	fetcher := h.SubPresets
	if fetcher == nil {
		fetcher = subpreset.NewClient()
	}
	snapshot, err := fetcher.Fetch(r.Context())
	if err != nil {
		finish(func(w http.ResponseWriter, r *http.Request) {
			fail(w, 502, "ACL4SSR synchronization failed; the cached library was retained")
		})
		return
	}
	library := store.SubPresetLibrary{Revision: snapshot.Revision, SyncedAt: snapshot.SyncedAt, Items: []store.SubDesignPreset{}}
	for _, file := range snapshot.Files {
		key := "acl4ssr:" + file.Name
		item := store.SubDesignPreset{Key: key, Name: strings.TrimSuffix(file.Name, ".ini"), Source: "acl4ssr", Revision: snapshot.Revision, URL: file.URL, Content: file.Content}
		design, parseErr := subdesign.ParseACL4SSR(file.Name, file.Content)
		if parseErr == nil {
			design.Source = &subdesign.PresetSource{Key: key, Name: item.Name, Revision: snapshot.Revision, URL: file.URL}
			parseErr = validateSubDesign(&design)
		}
		if parseErr != nil {
			item.Reason = parseErr.Error()
		} else {
			item.Design = &design
			for _, f := range design.Support() {
				if f.Supported {
					item.Supported = true
				}
			}
			if !item.Supported {
				item.Reason = "no supported output format"
			}
		}
		library.Items = append(library.Items, item)
	}
	finish(func(w http.ResponseWriter, r *http.Request) {
		if err := h.Store.SetSetting(r.Context(), store.SettingSubPresetLibrary, library); err != nil {
			serverErr(w, err)
			return
		}
		ok(w, subLibraryInfo{Revision: library.Revision, SyncedAt: library.SyncedAt, Count: len(library.Items)})
	})
}

func (h *handlers) saveSubCustomPreset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string           `json:"name"`
		Design subdesign.Design `json:"design"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 120 || !spec.PublicLabel(in.Name) {
		fail(w, 400, "preset name must be 1–120 plain characters")
		return
	}
	if err := validateSubDesign(&in.Design); err != nil {
		fail(w, 400, err.Error())
		return
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		serverErr(w, err)
		return
	}
	key := "custom:" + hex.EncodeToString(entropy[:])
	// Preserve the original upstream URL/revision for attribution even when a
	// modified design is saved under a local name.
	if in.Design.Source == nil {
		in.Design.Source = &subdesign.PresetSource{}
	}
	in.Design.Source.Key, in.Design.Source.Name = key, in.Name
	if err := validateSubDesign(&in.Design); err != nil {
		fail(w, 400, err.Error())
		return
	}
	item := store.SubDesignPreset{Key: key, Name: in.Name, Source: "custom", Revision: in.Design.Source.Revision, URL: in.Design.Source.URL, Supported: true, Design: &in.Design}
	if err := h.Store.SaveSubCustomPreset(r.Context(), item); err != nil {
		if errors.Is(err, store.ErrSubPresetName) || errors.Is(err, store.ErrSubPresetLimit) {
			fail(w, 409, err.Error())
		} else {
			serverErr(w, err)
		}
		return
	}
	ok(w, presetInfo(item))
}

func (h *handlers) deleteSubCustomPreset(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	if !strings.HasPrefix(key, "custom:") {
		fail(w, 400, "only custom presets can be deleted")
		return
	}
	if err := h.Store.DeleteSubCustomPreset(r.Context(), key); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
