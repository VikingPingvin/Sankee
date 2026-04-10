package main

import "log"

// DebugPopulate seeds the store with a realistic household budget so the
// Sankey diagram is immediately useful for testing and development.
//
// Structure (20 nodes, income £3 500/mo):
//
//	Income (£3 500)
//	├── Housing            → Rent, Electricity, Gas, Council Tax
//	├── Food               → Groceries, Eating Out
//	├── Transport
//	│   ├── Car            → Insurance, Fuel        (sub-category)
//	│   └── Public Transport
//	├── Entertainment      → Streaming, Hobbies
//	├── Personal           → Clothing, Subscriptions
//	└── Savings            → Emergency Fund, Investments
func DebugPopulate(s *Store) {
	must := func(n Node, err error) Node {
		if err != nil {
			log.Fatalf("seed: %v", err)
		}
		return n
	}

	s.SetIncomeAmount(3500)

	// ── Housing ──────────────────────────────────────────────────────────────
	housing := must(s.Create("Housing", "income", 0))
	must(s.Create("Rent", housing.ID, 950))
	must(s.Create("Electricity", housing.ID, 75))
	must(s.Create("Gas", housing.ID, 50))
	must(s.Create("Council Tax", housing.ID, 120))

	// ── Food ─────────────────────────────────────────────────────────────────
	food := must(s.Create("Food", "income", 0))
	must(s.Create("Groceries", food.ID, 310))
	must(s.Create("Eating Out", food.ID, 120))

	// ── Transport (with Car sub-category) ────────────────────────────────────
	transport := must(s.Create("Transport", "income", 0))
	car := must(s.Create("Car", transport.ID, 0))
	must(s.Create("Insurance", car.ID, 90))
	must(s.Create("Fuel", car.ID, 105))
	must(s.Create("Public Transport", transport.ID, 70))

	// ── Entertainment ─────────────────────────────────────────────────────────
	entertainment := must(s.Create("Entertainment", "income", 0))
	must(s.Create("Streaming", entertainment.ID, 25))
	must(s.Create("Hobbies", entertainment.ID, 115))

	// ── Personal ─────────────────────────────────────────────────────────────
	personal := must(s.Create("Personal", "income", 0))
	must(s.Create("Clothing", personal.ID, 75))
	must(s.Create("Subscriptions", personal.ID, 60))

	// ── Savings ──────────────────────────────────────────────────────────────
	savings := must(s.Create("Savings", "income", 0))
	must(s.Create("Emergency Fund", savings.ID, 200))
	must(s.Create("Investments", savings.ID, 150))

	// Allocated: £2 515  Unallocated: £985
	log.Println("seed: store pre-populated with sample budget (£3 500 income, £2 515 allocated)")
}
