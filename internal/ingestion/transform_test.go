package ingestion

import (
	"testing"

	"github.com/natalie-johanek/trench-analytics/internal/models"
)

func makeReport(mods ...func(*models.SynodReport)) *models.SynodReport {
	r := &models.SynodReport{
		GameReportID: 1,
		PlayerIDs:    []int{100, 200},
		WarbandIDs:   []int{10, 20},
		Data: models.SynodReportData{
			Winner:     intPtr(10),
			Kills:      map[string]int{"10": 3, "20": 2},
			VP:         map[string]int{"10": 1, "20": 0},
			ScenarioID: "sc_test",
			Date:       1700000000, // 2023-11-14
			Ranked:     true,
			Warbands: []models.SynodWarband{
				{
					FactionSlug: "fc_newantioch",
					WarbandExport: models.SynodExport{
						WID: 10, WNA: "Warband A", DR: 500, GR: 0,
						ELT: []models.SynodUnit{{MDN: "Leader", MDI: "md_leader", C: models.SynodCost{D: 100}}},
						MDS: []models.SynodUnit{{MDN: "Trooper", MDI: "md_trooper", C: models.SynodCost{D: 50}}},
					},
				},
				{
					FactionSlug: "fc_hereticlegion",
					WarbandExport: models.SynodExport{
						WID: 20, WNA: "Warband B", DR: 450, GR: 5,
						ELT: []models.SynodUnit{{MDN: "Dark Leader", MDI: "md_darkleader", C: models.SynodCost{D: 120}}},
						MDS: []models.SynodUnit{
							{MDN: "Heretic", MDI: "md_heretic", C: models.SynodCost{D: 40}},
							{MDN: "Heretic", MDI: "md_heretic", C: models.SynodCost{D: 40}},
						},
						MRC: []models.SynodUnit{{MDN: "Merc", MDI: "md_merc", C: models.SynodCost{D: 80}}},
					},
				},
			},
			Deeds: []models.SynodDeed{
				{ID: "gd_sniper", Name: "Sniper", Warband: intPtr(10)},
				{ID: "gd_brave", Name: "Brave"},
			},
		},
	}
	for _, m := range mods {
		m(r)
	}
	return r
}

func intPtr(v int) *int { return &v }

func TestTransform_HappyPath(t *testing.T) {
	r := makeReport()
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Game
	if result.Game.GameReportID != 1 {
		t.Errorf("game_report_id = %d, want 1", result.Game.GameReportID)
	}
	if result.Game.IsDraw {
		t.Error("expected not a draw")
	}
	if result.Game.ScenarioID != "sc_test" {
		t.Errorf("scenario = %q, want sc_test", result.Game.ScenarioID)
	}

	// Participants
	if len(result.Participants) != 2 {
		t.Fatalf("participants = %d, want 2", len(result.Participants))
	}
	if result.Participants[0].Result != "win" {
		t.Errorf("participant 0 result = %q, want win", result.Participants[0].Result)
	}
	if result.Participants[1].Result != "loss" {
		t.Errorf("participant 1 result = %q, want loss", result.Participants[1].Result)
	}
	if result.Participants[0].Kills != 3 {
		t.Errorf("participant 0 kills = %d, want 3", result.Participants[0].Kills)
	}

	// Units — warband B has 1 elt + 2 mds + 1 mrc = 4 units, global row_index 0-3
	wbBUnits := 0
	for _, u := range result.Units {
		if u.WarbandID == 20 {
			if u.RowIndex != wbBUnits {
				t.Errorf("warband B unit row_index = %d, want %d", u.RowIndex, wbBUnits)
			}
			wbBUnits++
		}
	}
	if wbBUnits != 4 {
		t.Errorf("warband B units = %d, want 4", wbBUnits)
	}

	// Unit types
	for _, u := range result.Units {
		if u.WarbandID == 20 {
			switch u.RowIndex {
			case 0:
				if u.UnitType != "elite" {
					t.Errorf("row 0 type = %q, want elite", u.UnitType)
				}
			case 1, 2:
				if u.UnitType != "standard" {
					t.Errorf("row %d type = %q, want standard", u.RowIndex, u.UnitType)
				}
			case 3:
				if u.UnitType != "mercenary" {
					t.Errorf("row 3 type = %q, want mercenary", u.UnitType)
				}
			}
		}
	}

	// Deeds
	if len(result.Deeds) != 2 {
		t.Fatalf("deeds = %d, want 2", len(result.Deeds))
	}
	if result.Deeds[0].DeedID != "gd_sniper" {
		t.Errorf("deed 0 id = %q, want gd_sniper", result.Deeds[0].DeedID)
	}
}

func TestTransform_Draw(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		r.Data.Winner = nil
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Game.IsDraw {
		t.Error("expected draw")
	}
	for _, p := range result.Participants {
		if p.Result != "draw" {
			t.Errorf("participant result = %q, want draw", p.Result)
		}
	}
}

func TestTransform_DrawZeroWinner(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		zero := 0
		r.Data.Winner = &zero
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Game.IsDraw {
		t.Error("expected draw for winner=0")
	}
}

func TestTransform_Validations(t *testing.T) {
	tests := []struct {
		name string
		mod  func(*models.SynodReport)
	}{
		{"one warband", func(r *models.SynodReport) { r.Data.Warbands = r.Data.Warbands[:1] }},
		{"three warbands", func(r *models.SynodReport) { r.Data.Warbands = append(r.Data.Warbands, r.Data.Warbands[0]) }},
		{"zero warbands", func(r *models.SynodReport) { r.Data.Warbands = nil }},
		{"bad winner", func(r *models.SynodReport) { w := 99999; r.Data.Winner = &w }},
		{"empty faction", func(r *models.SynodReport) { r.Data.Warbands[0].FactionSlug = "" }},
		{"no units", func(r *models.SynodReport) {
			r.Data.Warbands[0].WarbandExport.ELT = nil
			r.Data.Warbands[0].WarbandExport.MDS = nil
			r.Data.Warbands[0].WarbandExport.MRC = nil
		}},
		{"old date", func(r *models.SynodReport) { r.Data.Date = 0 }},
		{"future date", func(r *models.SynodReport) { r.Data.Date = 9999999999 }},
		{"empty scenario", func(r *models.SynodReport) { r.Data.ScenarioID = "" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := makeReport(tt.mod)
			_, err := Transform(r)
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestTransform_FewPlayerIDs(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		r.PlayerIDs = []int{1} // fewer than 2
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should default to [0, 0] and still succeed
	if result.Participants[0].PlayerID != 0 {
		t.Errorf("player_id = %d, want 0 (defaulted)", result.Participants[0].PlayerID)
	}
}

func TestTransform_EmptyPlayerIDs(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		r.PlayerIDs = nil
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Participants[0].PlayerID != 0 {
		t.Errorf("player_id = %d, want 0 (defaulted)", result.Participants[0].PlayerID)
	}
}

func TestTransform_WinnerIsSecondWarband(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		*r.Data.Winner = 20 // second warband wins
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Participants[0].Result != "loss" {
		t.Errorf("participant 0 result = %q, want loss", result.Participants[0].Result)
	}
	if result.Participants[1].Result != "win" {
		t.Errorf("participant 1 result = %q, want win", result.Participants[1].Result)
	}
}

func TestTransform_NoDeeds(t *testing.T) {
	r := makeReport(func(r *models.SynodReport) {
		r.Data.Deeds = nil
	})
	result, err := Transform(r)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Deeds) != 0 {
		t.Errorf("deeds = %d, want 0", len(result.Deeds))
	}
}
