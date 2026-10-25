package http

import (
	"bytes"
	"context"
	"testing"

	"github.com/zeptop-dev/captain/internal/domain"
	"github.com/zeptop-dev/captain/internal/http/admin"
)

func TestEntryCategoryAPICompatibilityAndSubscriptionIsolation(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	_, subPath, _ := profileSetup(t, r)
	call := func(method, path string, payload any, want int) []byte {
		t.Helper()
		status, body, _ := r.c.do(method, path, payload, nil)
		if status != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, status, want, body)
		}
		return body
	}
	before := call("GET", subPath+"?client=clash", nil, 200)
	entries, err := r.st.ListEntries(ctx)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	e := entries[0]
	base := "/api/admin/entry-categories"
	c := mustJSON[domain.EntryCategory](t, call("POST", base, map[string]any{"Name": "Premium", "Description": "Display category", "Sort": 999}, 200))
	if c.Sort != 10 {
		t.Fatal("arbitrary creation order accepted", c)
	}
	call("POST", base, map[string]any{"Name": " premium "}, 409)
	call("PATCH", base+"/"+itoa(c.ID), map[string]any{"Description": "Updated description"}, 200)
	list := mustJSON[[]domain.EntryCategory](t, call("GET", base, nil, 200))
	if len(list) != 1 || list[0].Description != "Updated description" || list[0].Name != "Premium" {
		t.Fatal("partial category update", list)
	}
	entryPath := "/api/admin/entries/" + itoa(e.ID)
	getCategory := func(body []byte, want int64) {
		t.Helper()
		got := mustJSON[domain.Entry](t, body)
		if want == 0 && got.CategoryID != nil || want != 0 && (got.CategoryID == nil || *got.CategoryID != want) {
			t.Fatalf("CategoryID: want %d, got %+v", want, got)
		}
	}
	getCategory(call("PATCH", entryPath, map[string]any{"CategoryID": c.ID}, 200), c.ID)
	// Legacy PATCH payloads and null preserve the category; zero alone clears it.
	getCategory(call("PATCH", entryPath, map[string]any{"Name": e.Name}, 200), c.ID)
	getCategory(call("PATCH", entryPath, map[string]any{"CategoryID": nil}, 200), c.ID)
	getCategory(call("PATCH", entryPath, map[string]any{"CategoryID": 0}, 200), 0)
	for _, payload := range []map[string]any{{"IDs": []int64{e.ID}}, {"IDs": []int64{e.ID}, "CategoryID": nil}, {"IDs": []int64{e.ID}, "CategoryID": -1}, {"IDs": []int64{e.ID, 999999}, "CategoryID": c.ID}} {
		call("PUT", "/api/admin/entries/category", payload, 400)
	}
	got, err := r.st.EntryByID(ctx, e.ID)
	if err != nil || got.CategoryID != nil {
		t.Fatal("failed assignment leaked a partial result", got, err)
	}
	call("PUT", "/api/admin/entries/category", map[string]any{"IDs": []int64{e.ID}, "CategoryID": c.ID}, 200)
	for _, invalid := range []int64{-1, 999999} {
		call("PATCH", entryPath, map[string]any{"CategoryID": invalid}, 400)
	}
	got, err = r.st.EntryByID(ctx, e.ID)
	if err != nil || got.CategoryID == nil || *got.CategoryID != c.ID {
		t.Fatal("failed editor update lost category", got, err)
	}
	call("PUT", base+"/order", map[string]any{"IDs": []int64{c.ID}}, 200)
	after := call("GET", subPath+"?client=clash", nil, 200)
	if !bytes.Equal(before, after) {
		t.Fatal("display-only category changed rendered subscription")
	}
	call("DELETE", base+"/"+itoa(c.ID), nil, 200)
	got, err = r.st.EntryByID(ctx, e.ID)
	if err != nil || got.CategoryID != nil || got.Name != e.Name || got.Sort != e.Sort {
		t.Fatal("deletion changed entry beyond category", got, err)
	}
	call("DELETE", base+"/"+itoa(c.ID), nil, 404)
}

func TestEntryCategoryAPIAuthorization(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	requests := []struct{ method, path string }{
		{"GET", "/api/admin/entry-categories"},
		{"POST", "/api/admin/entry-categories"},
		{"PATCH", "/api/admin/entry-categories/1"},
		{"DELETE", "/api/admin/entry-categories/1"},
		{"PUT", "/api/admin/entry-categories/order"},
		{"PUT", "/api/admin/entries/category"},
	}
	visitor := &client{t: t, srv: r.srv}
	for _, req := range requests {
		if status, _, _ := visitor.do(req.method, req.path, map[string]any{}, nil); status != 401 {
			t.Fatal("unauthenticated access allowed", req, status)
		}
	}
	for _, role := range []string{domain.RoleOperator, domain.RoleSupport} {
		u, err := admin.NewUser(role+"-categories@example.com", "password123", role)
		if err != nil {
			t.Fatal(err)
		}
		if err := r.st.CreateStaff(ctx, u); err != nil {
			t.Fatal(err)
		}
		c := &client{t: t, srv: r.srv}
		if status, _, _ := c.do("POST", "/api/admin/login", map[string]string{"Email": u.Email, "Password": "password123"}, nil); status != 200 {
			t.Fatal("login failed", status)
		}
		if role == domain.RoleOperator {
			if status, body, _ := c.do("POST", "/api/admin/entry-categories", map[string]string{"Name": "Operator category"}, nil); status != 200 {
				t.Fatal("operator cannot manage entry display metadata", status, string(body))
			}
			continue
		}
		for _, req := range requests {
			if status, _, _ := c.do(req.method, req.path, map[string]any{}, nil); status != 403 {
				t.Fatal("support accessed categories", req, status)
			}
		}
	}
}
