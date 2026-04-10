package main

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"sync"
)

// TemplateData is passed to both the "page" and "table" templates.
type TemplateData struct {
	Nodes               []NodeView
	IncomeAmount        float64
	UnallocatedAmount   float64
	Currency            string
	Currencies          []CurrencyOption
	Error               string
	FocusNodeID         string // ID of a newly created node whose label should be auto-focused
	Workspaces          []Workspace
	ActiveWorkspaceID   string
	ActiveWorkspaceName string
}

// Handlers holds shared dependencies for HTTP handlers.
type Handlers struct {
	db       *DB
	stores   map[string]*Store
	storesMu sync.RWMutex
	tmpl     *template.Template
}

// NewHandlers constructs a Handlers with the given database and template.
func NewHandlers(db *DB, tmpl *template.Template) *Handlers {
	return &Handlers{
		db:     db,
		stores: make(map[string]*Store),
		tmpl:   tmpl,
	}
}

// getStore returns the in-memory Store for workspaceID, loading it from the
// database on first access.
func (h *Handlers) getStore(workspaceID string) (*Store, error) {
	h.storesMu.RLock()
	s, ok := h.stores[workspaceID]
	h.storesMu.RUnlock()
	if ok {
		return s, nil
	}
	h.storesMu.Lock()
	defer h.storesMu.Unlock()
	// Double-check after acquiring the write lock.
	if s, ok = h.stores[workspaceID]; ok {
		return s, nil
	}
	s, err := LoadStore(h.db, workspaceID)
	if err != nil {
		return nil, err
	}
	h.stores[workspaceID] = s
	return s, nil
}

// activeWorkspaceID reads the workspace cookie and validates it against the
// known workspace list. Falls back to the first workspace in the list.
func (h *Handlers) activeWorkspaceID(r *http.Request, workspaces []Workspace) string {
	if c, err := r.Cookie("workspace_id"); err == nil && c.Value != "" {
		for _, w := range workspaces {
			if w.ID == c.Value {
				return c.Value
			}
		}
	}
	if len(workspaces) > 0 {
		return workspaces[0].ID
	}
	return ""
}

// setWorkspaceCookie writes the active workspace cookie to the response.
func setWorkspaceCookie(w http.ResponseWriter, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "workspace_id",
		Value:    id,
		Path:     "/",
		HttpOnly: true,
	})
}

// resolveRequest loads the workspace list and the active store for the request.
func (h *Handlers) resolveRequest(r *http.Request) (wsID string, store *Store, workspaces []Workspace, err error) {
	workspaces, err = dbListWorkspaces(h.db)
	if err != nil {
		return
	}
	wsID = h.activeWorkspaceID(r, workspaces)
	store, err = h.getStore(wsID)
	return
}

// buildTemplateData assembles TemplateData from the current store and workspace state.
func (h *Handlers) buildTemplateData(store *Store, workspaces []Workspace, wsID, errMsg, focusNodeID string) TemplateData {
	var activeName string
	for _, w := range workspaces {
		if w.ID == wsID {
			activeName = w.Name
			break
		}
	}
	return TemplateData{
		Nodes:               store.AllViews(),
		IncomeAmount:        store.IncomeAmount(),
		UnallocatedAmount:   store.UnallocatedAmount(),
		Currency:            store.Currency(),
		Currencies:          Currencies,
		Error:               errMsg,
		FocusNodeID:         focusNodeID,
		Workspaces:          workspaces,
		ActiveWorkspaceID:   wsID,
		ActiveWorkspaceName: activeName,
	}
}

// renderTable writes the "table" template fragment to w.
func (h *Handlers) renderTable(w http.ResponseWriter, store *Store, workspaces []Workspace, wsID string) {
	h.tmpl.ExecuteTemplate(w, "table", h.buildTemplateData(store, workspaces, wsID, "", ""))
}

// renderTableWithError writes the "table" template fragment with an error banner.
func (h *Handlers) renderTableWithError(w http.ResponseWriter, store *Store, workspaces []Workspace, wsID, errMsg string) {
	h.tmpl.ExecuteTemplate(w, "table", h.buildTemplateData(store, workspaces, wsID, errMsg, ""))
}

// Index renders the full page. If a ?ws= query param is present the workspace
// cookie is updated and the browser is redirected to / (clean URL).
func (h *Handlers) Index(w http.ResponseWriter, r *http.Request) {
	if ws := r.URL.Query().Get("ws"); ws != "" {
		setWorkspaceCookie(w, ws)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	h.tmpl.ExecuteTemplate(w, "page", h.buildTemplateData(store, workspaces, wsID, "", ""))
}

// Nodes returns the table fragment (used for initial HTMX load if needed).
func (h *Handlers) Nodes(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	h.renderTable(w, store, workspaces, wsID)
}

// SankeyData returns the Sankey JSON payload for D3.
func (h *Handlers) SankeyData(w http.ResponseWriter, r *http.Request) {
	_, store, _, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(store.SankeyData())
}

// SetCurrency handles PUT /currency.
func (h *Handlers) SetCurrency(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	r.ParseForm()
	if err := store.SetCurrency(r.FormValue("symbol")); err != nil {
		h.renderTableWithError(w, store, workspaces, wsID, err.Error())
		return
	}
	h.renderTable(w, store, workspaces, wsID)
}

// SetIncome handles PUT /income.
func (h *Handlers) SetIncome(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	r.ParseForm()
	amount, err := strconv.ParseFloat(r.FormValue("amount"), 64)
	if err != nil || amount < 0 {
		h.renderTableWithError(w, store, workspaces, wsID, "Invalid income amount")
		return
	}
	if err := store.SetIncomeAmount(amount); err != nil {
		h.renderTableWithError(w, store, workspaces, wsID, err.Error())
		return
	}
	h.renderTable(w, store, workspaces, wsID)
}

// CreateNode handles POST /nodes.
func (h *Handlers) CreateNode(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	r.ParseForm()
	label := r.FormValue("label")
	parentID := r.FormValue("parentID")
	if label == "" || parentID == "" {
		http.Error(w, "label and parentID are required", http.StatusBadRequest)
		return
	}
	amount, _ := strconv.ParseFloat(r.FormValue("amount"), 64)
	node, err := store.Create(label, parentID, amount)
	if err != nil {
		h.renderTableWithError(w, store, workspaces, wsID, err.Error())
		return
	}
	h.tmpl.ExecuteTemplate(w, "table", h.buildTemplateData(store, workspaces, wsID, "", node.ID))
}

// UpdateNode handles PUT /nodes/{id}.
func (h *Handlers) UpdateNode(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	r.ParseForm()
	label := r.FormValue("label")
	parentID := r.FormValue("parentID")
	amount, _ := strconv.ParseFloat(r.FormValue("amount"), 64)
	if _, err := store.Update(id, label, parentID, amount); err != nil {
		h.renderTableWithError(w, store, workspaces, wsID, err.Error())
		return
	}
	h.renderTable(w, store, workspaces, wsID)
}

// DeleteNode handles DELETE /nodes/{id}.
func (h *Handlers) DeleteNode(w http.ResponseWriter, r *http.Request) {
	wsID, store, workspaces, err := h.resolveRequest(r)
	if err != nil {
		http.Error(w, "failed to load workspace", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	if err := store.Delete(id); err != nil {
		h.renderTableWithError(w, store, workspaces, wsID, err.Error())
		return
	}
	h.renderTable(w, store, workspaces, wsID)
}

// CreateWorkspace handles POST /workspaces — creates a named workspace, sets
// the cookie, and redirects the browser to load the new workspace.
func (h *Handlers) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "workspace name is required", http.StatusBadRequest)
		return
	}
	id := newID()
	if err := dbCreateWorkspace(h.db, id, name); err != nil {
		http.Error(w, "failed to create workspace: "+err.Error(), http.StatusInternalServerError)
		return
	}
	setWorkspaceCookie(w, id)
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// DeleteWorkspace handles DELETE /workspaces/{id} — deletes the workspace and
// redirects to another available workspace.
func (h *Handlers) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	workspaces, err := dbListWorkspaces(h.db)
	if err != nil {
		http.Error(w, "failed to list workspaces", http.StatusInternalServerError)
		return
	}
	if len(workspaces) <= 1 {
		http.Error(w, "cannot delete the last workspace", http.StatusBadRequest)
		return
	}

	if err := dbDeleteWorkspace(h.db, id); err != nil {
		http.Error(w, "failed to delete workspace: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Evict the deleted store from the cache.
	h.storesMu.Lock()
	delete(h.stores, id)
	h.storesMu.Unlock()

	// If the deleted workspace was active, switch to the first remaining one.
	activeID := h.activeWorkspaceID(r, workspaces)
	if activeID == id {
		for _, ws := range workspaces {
			if ws.ID != id {
				setWorkspaceCookie(w, ws.ID)
				break
			}
		}
	}

	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}
