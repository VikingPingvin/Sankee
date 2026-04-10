package main

import (
	"testing"
)

func TestNewStore_hasIncomeRoot(t *testing.T) {
	s := NewStore()
	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 node, got %d", len(all))
	}
	if all[0].ID != "income" || all[0].Label != "Income" {
		t.Fatalf("expected income root, got %+v", all[0])
	}
}

func TestCreate_addsNode(t *testing.T) {
	s := NewStore()
	n, err := s.Create("Shopping", "income", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n.Label != "Shopping" || n.ParentID != "income" {
		t.Fatalf("unexpected node: %+v", n)
	}
	all := s.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(all))
	}
}

func TestUpdate_changesFields(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(500)
	n, _ := s.Create("Shopping", "income", 100)
	ok, err := s.Update(n.ID, "Groceries", "income", 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("Update returned false")
	}
	updated, found := s.Get(n.ID)
	if !found {
		t.Fatal("node not found after update")
	}
	if updated.Label != "Groceries" || updated.Amount != 200 {
		t.Fatalf("unexpected node: %+v", updated)
	}
}

func TestUpdate_incomeCannotBeReparented(t *testing.T) {
	s := NewStore()
	s.Update("income", "My Income", "someother", 999)
	node, _ := s.Get("income")
	if node.ParentID != "" {
		t.Fatalf("income parentID should stay empty, got %q", node.ParentID)
	}
	if node.Label != "My Income" {
		t.Fatalf("income label should update, got %q", node.Label)
	}
}

func TestDelete_removesNodeAndDescendants(t *testing.T) {
	s := NewStore()
	cat, _ := s.Create("Shopping", "income", 0)
	_, _ = s.Create("Amazon", cat.ID, 0)

	s.Delete(cat.ID)

	all := s.All()
	if len(all) != 1 {
		t.Fatalf("expected 1 node after delete, got %d: %+v", len(all), all)
	}
	if all[0].ID != "income" {
		t.Fatalf("expected income node to remain")
	}
}

func TestDelete_incomeIsProtected(t *testing.T) {
	s := NewStore()
	s.Delete("income")
	all := s.All()
	if len(all) != 1 || all[0].ID != "income" {
		t.Fatal("income should not be deletable")
	}
}

func TestEffectiveAmount_leafUsesOwnAmount(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(200)
	cat, _ := s.Create("Shopping", "income", 0)
	item, _ := s.Create("Amazon", cat.ID, 75)

	s.mu.RLock()
	defer s.mu.RUnlock()
	got := s.effectiveAmount(item.ID)
	if got != 75 {
		t.Fatalf("expected 75, got %f", got)
	}
}

func TestEffectiveAmount_parentSumsChildren(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(200)
	cat, _ := s.Create("Shopping", "income", 0)
	s.Create("Amazon", cat.ID, 50)
	s.Create("ASOS", cat.ID, 30)

	s.mu.RLock()
	defer s.mu.RUnlock()
	got := s.effectiveAmount(cat.ID)
	if got != 80 {
		t.Fatalf("expected 80, got %f", got)
	}
}

func TestCreate_exceedsIncome_returnsError(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(100)
	_, err := s.Create("BigExpense", "income", 150)
	if err == nil {
		t.Fatal("expected error when allocation exceeds income")
	}
	if len(s.All()) != 1 {
		t.Fatal("failed node must not be stored")
	}
}

func TestUpdate_exceedsIncome_returnsError(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(100)
	n, _ := s.Create("Shopping", "income", 80)
	_, err := s.Update(n.ID, "Shopping", "income", 150)
	if err == nil {
		t.Fatal("expected error when update would exceed income")
	}
	// Amount should be unchanged
	node, _ := s.Get(n.ID)
	if node.Amount != 80 {
		t.Fatalf("expected amount to be rolled back to 80, got %f", node.Amount)
	}
}

func TestUnallocatedAmount(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(1000)
	cat, _ := s.Create("Shopping", "income", 0)
	s.Create("Amazon", cat.ID, 300)

	got := s.UnallocatedAmount()
	if got != 700 {
		t.Fatalf("expected 700 unallocated, got %f", got)
	}
}

func TestSankeyData_structure(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(100)
	cat, _ := s.Create("Shopping", "income", 0)
	s.Create("Amazon", cat.ID, 100)

	d := s.SankeyData()

	// Income=100, allocated=100 → unallocated=0 → no virtual node added.
	// Expect 3 nodes: Income, Shopping, Amazon
	if len(d.Nodes) != 3 {
		t.Fatalf("expected 3 sankey nodes, got %d", len(d.Nodes))
	}
	// Expect 2 links: income->shopping, shopping->amazon
	if len(d.Links) != 2 {
		t.Fatalf("expected 2 sankey links, got %d", len(d.Links))
	}
	// All link values should be positive
	for _, l := range d.Links {
		if l.Value <= 0 {
			t.Fatalf("link value should be positive, got %f", l.Value)
		}
	}
}

func TestSankeyData_unallocatedNodeAdded(t *testing.T) {
	s := NewStore()
	s.SetIncomeAmount(1000)
	cat, _ := s.Create("Shopping", "income", 0)
	s.Create("Amazon", cat.ID, 300)

	d := s.SankeyData()

	// Expect 4 nodes: Income, Shopping, Amazon, Unallocated
	if len(d.Nodes) != 4 {
		t.Fatalf("expected 4 sankey nodes, got %d", len(d.Nodes))
	}
	// Expect 3 links: income->shopping, income->unallocated, shopping->amazon
	if len(d.Links) != 3 {
		t.Fatalf("expected 3 sankey links, got %d", len(d.Links))
	}
}

func TestSankeyData_emptyStore(t *testing.T) {
	s := NewStore()
	d := s.SankeyData()
	// Only income node, no links
	if len(d.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(d.Nodes))
	}
	if len(d.Links) != 0 {
		t.Fatalf("expected 0 links, got %d", len(d.Links))
	}
}
