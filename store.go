package main

import (
	"crypto/rand"
	"fmt"
	"sort"
	"strconv"
	"sync"
)

// Workspace is a named, isolated expense tracking context.
type Workspace struct {
	ID   string
	Name string
}

// Node represents a single expense tree node.
// ParentID is empty for the root (Income) node.
// Amount is used only for leaf nodes; non-leaf nodes derive their
// amount by summing children recursively.
type Node struct {
	ID       string
	Label    string
	ParentID string
	Amount   float64
}

// CurrencyOption is a selectable currency entry passed to templates.
type CurrencyOption struct {
	Symbol string
	Label  string
}

// Currencies is the list of supported currencies shown in the UI.
var Currencies = []CurrencyOption{
	{"£", "GBP £"},
	{"$", "USD $"},
	{"€", "EUR €"},
	{"HUF", "HUF Ft"},
	{"¥", "JPY ¥"},
	{"Fr", "CHF Fr"},
	{"C$", "CAD C$"},
	{"A$", "AUD A$"},
	{"₹", "INR ₹"},
	{"kr", "SEK kr"},
}

// NewStore creates an in-memory Store pre-seeded with the Income root node.
// Intended for tests; production code uses LoadStore with a real database.
func NewStore() *Store {
	s := &Store{
		workspaceID: "test",
		nodes:       make(map[string]Node),
		currency:    "£",
	}
	s.nodes["income"] = Node{ID: "income", Label: "Income"}
	return s
}

// Store is a thread-safe in-memory store of Nodes scoped to one workspace.
// db may be nil (tests, or in-memory-only mode).
type Store struct {
	mu           sync.RWMutex
	workspaceID  string
	nodes        map[string]Node
	incomeAmount float64
	currency     string
	db           *DB
}

// Currency returns the active currency symbol.
func (s *Store) Currency() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.currency
}

// SetCurrency sets the currency symbol. Returns an error for unknown symbols.
func (s *Store) SetCurrency(symbol string) error {
	for _, c := range Currencies {
		if c.Symbol == symbol {
			s.mu.Lock()
			defer s.mu.Unlock()
			old := s.currency
			s.currency = symbol
			if s.db != nil {
				if err := dbSaveSetting(s.db, s.workspaceID, "currency", symbol); err != nil {
					s.currency = old
					return fmt.Errorf("persist currency: %w", err)
				}
			}
			return nil
		}
	}
	return fmt.Errorf("unknown currency %q", symbol)
}

// newID returns a random 8-hex-char string.
func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// IncomeAmount returns the configured income amount.
func (s *Store) IncomeAmount() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.incomeAmount
}

// SetIncomeAmount sets the income amount. Returns an error if the new amount
// is less than the sum already allocated to direct children of income.
func (s *Store) SetIncomeAmount(amount float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	allocated := s.allocatedSum()
	if amount > 0 && amount < allocated {
		return fmt.Errorf("income £%.2f is less than already allocated £%.2f", amount, allocated)
	}
	old := s.incomeAmount
	s.incomeAmount = amount
	if s.db != nil {
		if err := dbSaveSetting(s.db, s.workspaceID, "income", strconv.FormatFloat(amount, 'f', -1, 64)); err != nil {
			s.incomeAmount = old
			return fmt.Errorf("persist income: %w", err)
		}
	}
	return nil
}

// UnallocatedAmount returns incomeAmount minus the sum of all direct children of income.
func (s *Store) UnallocatedAmount() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.unallocatedAmount()
}

// allocatedSum returns the sum of effectiveAmount for all direct children of income.
// Must be called with mu held.
func (s *Store) allocatedSum() float64 {
	var total float64
	for _, child := range s.children("income") {
		total += s.effectiveAmount(child.ID)
	}
	return total
}

// unallocatedAmount returns incomeAmount - allocatedSum.
// Must be called with mu held.
func (s *Store) unallocatedAmount() float64 {
	return s.incomeAmount - s.allocatedSum()
}

// All returns all nodes sorted by ID.
func (s *Store) All() []Node {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Node, 0, len(s.nodes))
	for _, n := range s.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get returns a node by ID.
func (s *Store) Get(id string) (Node, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	n, ok := s.nodes[id]
	return n, ok
}

// Create adds a new node and returns it. Returns an error if the addition
// would cause allocations to exceed the configured income amount.
func (s *Store) Create(label, parentID string, amount float64) (Node, error) {
	n := Node{ID: newID(), Label: label, ParentID: parentID, Amount: amount}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes[n.ID] = n
	if s.incomeAmount > 0 && s.allocatedSum() > s.incomeAmount {
		delete(s.nodes, n.ID)
		return Node{}, fmt.Errorf("allocation would exceed income of £%.2f", s.incomeAmount)
	}
	if s.db != nil {
		if err := dbUpsertNode(s.db, s.workspaceID, n); err != nil {
			delete(s.nodes, n.ID)
			return Node{}, fmt.Errorf("persist node: %w", err)
		}
	}
	return n, nil
}

// Update modifies an existing node. Returns false if not found, or an error
// if the change would cause allocations to exceed the configured income amount.
// The income root cannot be reparented or have its amount changed.
func (s *Store) Update(id, label, parentID string, amount float64) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	node, ok := s.nodes[id]
	if !ok {
		return false, nil
	}
	old := node
	node.Label = label
	if id != "income" {
		node.ParentID = parentID
		node.Amount = amount
	}
	s.nodes[id] = node
	if id != "income" && s.incomeAmount > 0 && s.allocatedSum() > s.incomeAmount {
		s.nodes[id] = old
		return false, fmt.Errorf("allocation would exceed income of £%.2f", s.incomeAmount)
	}
	if s.db != nil {
		if err := dbUpsertNode(s.db, s.workspaceID, node); err != nil {
			s.nodes[id] = old
			return false, fmt.Errorf("persist node: %w", err)
		}
	}
	return true, nil
}

// Delete removes a node and all its descendants. Income is protected.
func (s *Store) Delete(id string) error {
	if id == "income" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := s.collectSubtree(id)
	if s.db != nil {
		if err := dbDeleteNodes(s.db, s.workspaceID, ids); err != nil {
			return fmt.Errorf("persist delete: %w", err)
		}
	}
	for _, rid := range ids {
		delete(s.nodes, rid)
	}
	return nil
}

// collectSubtree returns id plus all descendant IDs. Must be called with mu held.
func (s *Store) collectSubtree(id string) []string {
	ids := []string{id}
	for _, child := range s.children(id) {
		ids = append(ids, s.collectSubtree(child.ID)...)
	}
	return ids
}

// children returns direct children of parentID. Must be called with mu held.
func (s *Store) children(parentID string) []Node {
	var out []Node
	for _, n := range s.nodes {
		if n.ParentID == parentID {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// effectiveAmount returns the amount for a node: for the income root, the
// configured incomeAmount; for leaves, their own Amount; for other parents,
// the recursive sum of children. Must be called with mu held.
func (s *Store) effectiveAmount(id string) float64 {
	if id == "income" {
		return s.incomeAmount
	}
	ch := s.children(id)
	if len(ch) == 0 {
		return s.nodes[id].Amount
	}
	var total float64
	for _, c := range ch {
		total += s.effectiveAmount(c.ID)
	}
	return total
}

// NodeView enriches a Node with computed display fields for templates.
type NodeView struct {
	Node
	EffectiveAmount float64
	HasChildren     bool
	IsVirtual       bool // true for the synthesised unallocated node
	Depth           int  // distance from income root (income=0, direct children=1, …)
	IndentPx        int  // pixel indent for the label column ((depth-1)*20, min 0)
}

// AllViews returns all nodes as NodeViews in depth-first order starting from
// the income root, so siblings are grouped together in the table.
// The virtual "Unallocated" node is appended last at depth 1.
func (s *Store) AllViews() []NodeView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	views := make([]NodeView, 0, len(s.nodes)+1)

	var walk func(id string, depth int)
	walk = func(id string, depth int) {
		node, ok := s.nodes[id]
		if !ok {
			return
		}
		ch := s.children(id)
		indentPx := 0
		if depth > 1 {
			indentPx = (depth - 1) * 20
		}
		views = append(views, NodeView{
			Node:            node,
			EffectiveAmount: s.effectiveAmount(id),
			HasChildren:     len(ch) > 0,
			Depth:           depth,
			IndentPx:        indentPx,
		})
		for _, child := range ch {
			walk(child.ID, depth+1)
		}
	}

	walk("income", 0)

	// Append virtual unallocated node — always present so the income total balances.
	views = append(views, NodeView{
		Node: Node{
			ID:       "unallocated",
			Label:    "Unallocated",
			ParentID: "income",
		},
		EffectiveAmount: s.unallocatedAmount(),
		IsVirtual:       true,
		Depth:           1,
	})
	return views
}

// SankeyNode is a node in the D3 Sankey data format.
type SankeyNode struct {
	Name   string `json:"name"`
	IsRoot bool   `json:"isRoot,omitempty"`
}

// SankeyLink is a directed link in the D3 Sankey data format.
type SankeyLink struct {
	Source int     `json:"source"`
	Target int     `json:"target"`
	Value  float64 `json:"value"`
}

// SankeyData is the JSON payload consumed by the D3 Sankey renderer.
type SankeyData struct {
	Nodes    []SankeyNode `json:"nodes"`
	Links    []SankeyLink `json:"links"`
	Currency string       `json:"currency"`
}

// SankeyData builds the D3 Sankey payload from the current node tree.
// Nodes are indexed in BFS order starting from the income root.
// An "Unallocated" sink node is appended whenever unallocated income is positive,
// keeping the income source balanced without inflating it.
func (s *Store) SankeyData() SankeyData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	indexMap := map[string]int{}
	var sankeyNodes []SankeyNode
	var sankeyLinks []SankeyLink

	queue := []string{"income"}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		node, ok := s.nodes[id]
		if !ok {
			continue
		}
		idx := len(sankeyNodes)
		indexMap[id] = idx
		sankeyNodes = append(sankeyNodes, SankeyNode{Name: node.Label, IsRoot: id == "income"})
		for _, child := range s.children(id) {
			queue = append(queue, child.ID)
		}
	}

	// Add virtual unallocated sink when positive (D3 Sankey requires value > 0).
	if unalloc := s.unallocatedAmount(); unalloc > 0 {
		if incomeIdx, ok := indexMap["income"]; ok {
			unallocIdx := len(sankeyNodes)
			sankeyNodes = append(sankeyNodes, SankeyNode{Name: "Unallocated"})
			sankeyLinks = append(sankeyLinks, SankeyLink{
				Source: incomeIdx,
				Target: unallocIdx,
				Value:  unalloc,
			})
		}
	}

	for parentID, parentIdx := range indexMap {
		for _, child := range s.children(parentID) {
			childIdx, ok := indexMap[child.ID]
			if !ok {
				continue
			}
			sankeyLinks = append(sankeyLinks, SankeyLink{
				Source: parentIdx,
				Target: childIdx,
				Value:  s.effectiveAmount(child.ID),
			})
		}
	}

	return SankeyData{Nodes: sankeyNodes, Links: sankeyLinks, Currency: s.currency}
}
