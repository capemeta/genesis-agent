package contract

import "testing"

func TestTreeBudgetAccumulatesAcrossDescendants(t *testing.T) {
	budget := NewTreeBudget(100, 3)
	if err := budget.Consume(40, 1); err != nil {
		t.Fatal(err)
	}
	if err := budget.Consume(50, 2); err != nil {
		t.Fatal(err)
	}
	if err := budget.Consume(11, 0); err == nil {
		t.Fatal("expected cumulative token budget to fail")
	}
	if err := budget.Consume(0, 1); err == nil {
		t.Fatal("expected cumulative tool-call budget to fail")
	}
}
