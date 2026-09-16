package admin

import (
	"net/http"
	"strings"
	"time"

	"github.com/zeptop-dev/captain/internal/domain"
)

type articleView struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Category  string    `json:"category"`
	Body      string    `json:"body"`
	Lang      string    `json:"lang"`
	Sort      int       `json:"sort"`
	Published bool      `json:"published"`
	UpdatedAt time.Time `json:"updated_at"`
}

func toArticleView(a *domain.Article) articleView {
	return articleView{ID: a.ID, Title: a.Title, Category: a.Category, Body: a.Body, Lang: a.Lang, Sort: a.Sort, Published: a.Published, UpdatedAt: a.UpdatedAt}
}

func (h *handlers) listArticles(w http.ResponseWriter, r *http.Request) {
	list, err := h.Store.ListArticles(r.Context(), false)
	if err != nil {
		serverErr(w, err)
		return
	}
	out := make([]articleView, 0, len(list))
	for _, a := range list {
		out = append(out, toArticleView(a))
	}
	ok(w, out)
}

func decodeArticle(r *http.Request, a *domain.Article) bool {
	var in struct {
		Title, Category, Body, Lang string
		Sort                        int
		Published                   *bool
	}
	if !decode(r, &in) || strings.TrimSpace(in.Title) == "" {
		return false
	}
	a.Title, a.Category, a.Body, a.Lang, a.Sort = strings.TrimSpace(in.Title), strings.TrimSpace(in.Category), in.Body, in.Lang, in.Sort
	if in.Published != nil {
		a.Published = *in.Published
	}
	return true
}

func (h *handlers) createArticle(w http.ResponseWriter, r *http.Request) {
	a := domain.Article{Published: true}
	if !decodeArticle(r, &a) {
		fail(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := h.Store.CreateArticle(r.Context(), &a); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, toArticleView(&a))
}

func (h *handlers) updateArticle(w http.ResponseWriter, r *http.Request) {
	a, err := h.Store.ArticleByID(r.Context(), idOf(r))
	if err != nil {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if !decodeArticle(r, a) {
		fail(w, http.StatusBadRequest, "title is required")
		return
	}
	if err := h.Store.UpdateArticle(r.Context(), a); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, toArticleView(a))
}

func (h *handlers) deleteArticle(w http.ResponseWriter, r *http.Request) {
	if err := h.Store.DeleteArticle(r.Context(), idOf(r)); err != nil {
		serverErr(w, err)
		return
	}
	ok(w, map[string]bool{"ok": true})
}
