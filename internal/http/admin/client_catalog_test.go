package admin

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientCatalogInternalErrorsAreNotExposed(t *testing.T) {
	w := httptest.NewRecorder()
	clientCatalogError(w, errors.New("sqlite internal path /private/example.db: sensitive statement"))
	if w.Code != 500 || strings.Contains(w.Body.String(), "example.db") || strings.Contains(w.Body.String(), "sensitive") {
		t.Fatal(w.Code, w.Body.String())
	}
}
