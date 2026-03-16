package models

import "testing"

func TestAllUnits(t *testing.T) {
	e := SynodExport{
		ELT: []SynodUnit{{MDN: "Leader", MDI: "md_leader"}},
		MDS: []SynodUnit{{MDN: "Trooper", MDI: "md_trooper"}, {MDN: "Trooper2", MDI: "md_trooper2"}},
		MRC: []SynodUnit{{MDN: "Merc", MDI: "md_merc"}},
	}
	units := e.AllUnits()
	if len(units) != 4 {
		t.Fatalf("units = %d, want 4", len(units))
	}
	if units[0].Type != "elite" {
		t.Errorf("unit 0 type = %q, want elite", units[0].Type)
	}
	if units[1].Type != "standard" {
		t.Errorf("unit 1 type = %q, want standard", units[1].Type)
	}
	if units[3].Type != "mercenary" {
		t.Errorf("unit 3 type = %q, want mercenary", units[3].Type)
	}
}

func TestAllUnits_Empty(t *testing.T) {
	e := SynodExport{}
	units := e.AllUnits()
	if len(units) != 0 {
		t.Errorf("units = %d, want 0", len(units))
	}
}
