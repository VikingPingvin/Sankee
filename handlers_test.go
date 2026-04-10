package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubTmpl is a minimal template that satisfies handler rendering.
var stubTmpl = template.Must(template.New("").Parse(
	`{{define "page"}}page{{end}}{{define "table"}}table{{end}}`,
))

const testWorkspaceID = "default"

// newTestHandlers creates a Handlers backed by an in-memory SQLite database
// with a single "Default" workspace. It also returns the pre-loaded Store for
// that workspace so tests can seed and inspect state directly.
func newTestHandlers(t *testing.T) (*Handlers, *Store) {
	t.Helper()
	db, err := OpenDB("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := dbCreateWorkspace(db, testWorkspaceID, "Default"); err != nil {
		t.Fatalf("create test workspace: %v", err)
	}
	h := NewHandlers(db, stubTmpl)
	store, err := h.getStore(testWorkspaceID)
	if err != nil {
		t.Fatalf("load test store: %v", err)
	}
	return h, store
}

// withWS attaches the test workspace cookie to r so handlers resolve the right store.
func withWS(r *http.Request) *http.Request {
	r.AddCookie(&http.Cookie{Name: "workspace_id", Value: testWorkspaceID})
	return r
}

func TestSankeyDataHandler_returnsJSON(t *testing.T) {
	h, _ := newTestHandlers(t)
	req := withWS(httptest.NewRequest(http.MethodGet, "/api/sankey-data", nil))
	w := httptest.NewRecorder()
	h.SankeyData(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected JSON content-type, got %q", ct)
	}
	var d SankeyData
	if err := json.NewDecoder(w.Body).Decode(&d); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestCreateNodeHandler_addsNode(t *testing.T) {
	h, store := newTestHandlers(t)
	form := url.Values{"label": {"Shopping"}, "parentID": {"income"}, "amount": {"0"}}
	req := withWS(httptest.NewRequest(http.MethodPost, "/nodes", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.CreateNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	all := store.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(all))
	}
}

func TestCreateNodeHandler_missingLabel_returns400(t *testing.T) {
	h, _ := newTestHandlers(t)
	form := url.Values{"label": {""}, "parentID": {"income"}, "amount": {"0"}}
	req := withWS(httptest.NewRequest(http.MethodPost, "/nodes", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.CreateNode(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestCreateNodeHandler_exceedsIncome_returns200WithError(t *testing.T) {
	h, store := newTestHandlers(t)
	store.SetIncomeAmount(100)
	form := url.Values{"label": {"BigSpend"}, "parentID": {"income"}, "amount": {"200"}}
	req := withWS(httptest.NewRequest(http.MethodPost, "/nodes", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.CreateNode(w, req)

	// Returns 200 with the table fragment containing the error message.
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Node must not have been stored.
	if len(store.All()) != 1 {
		t.Fatal("node must not be stored when allocation exceeds income")
	}
}

func TestUpdateNodeHandler_updatesNode(t *testing.T) {
	h, store := newTestHandlers(t)
	store.SetIncomeAmount(500)
	n, _ := store.Create("Shopping", "income", 50)

	form := url.Values{"label": {"Groceries"}, "parentID": {"income"}, "amount": {"120"}}
	req := withWS(httptest.NewRequest(http.MethodPut, "/nodes/"+n.ID, strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetPathValue("id", n.ID)
	w := httptest.NewRecorder()
	h.UpdateNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	updated, _ := store.Get(n.ID)
	if updated.Label != "Groceries" || updated.Amount != 120 {
		t.Fatalf("unexpected node state: %+v", updated)
	}
}

func TestSetIncomeHandler_setsAmount(t *testing.T) {
	h, store := newTestHandlers(t)
	form := url.Values{"amount": {"2500.00"}}
	req := withWS(httptest.NewRequest(http.MethodPut, "/income", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.SetIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if store.IncomeAmount() != 2500.00 {
		t.Fatalf("expected income 2500, got %f", store.IncomeAmount())
	}
}

func TestSetIncomeHandler_belowAllocated_returns200WithError(t *testing.T) {
	h, store := newTestHandlers(t)
	store.SetIncomeAmount(1000)
	store.Create("Rent", "income", 800)

	form := url.Values{"amount": {"500"}}
	req := withWS(httptest.NewRequest(http.MethodPut, "/income", strings.NewReader(form.Encode())))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.SetIncome(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	// Income must not have changed.
	if store.IncomeAmount() != 1000 {
		t.Fatalf("income should remain 1000, got %f", store.IncomeAmount())
	}
}

func TestDeleteNodeHandler_removesNode(t *testing.T) {
	h, store := newTestHandlers(t)
	n, _ := store.Create("Shopping", "income", 0)

	req := withWS(httptest.NewRequest(http.MethodDelete, "/nodes/"+n.ID, nil))
	req.SetPathValue("id", n.ID)
	w := httptest.NewRecorder()
	h.DeleteNode(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	all := store.All()
	if len(all) != 1 || all[0].ID != "income" {
		t.Fatalf("expected only income after delete, got %+v", all)
	}
}
