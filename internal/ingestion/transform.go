package ingestion

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/models"
)

// TransformResult holds the normalized rows derived from a single raw report.
type TransformResult struct {
	Game         models.Game
	Participants []models.GameParticipant
	Units        []models.GameUnit
	Deeds        []models.GameDeed
}

// Transform converts a raw Synod report into normalized DB rows.
// Returns an error if the report fails validation (caller should skip it).
func Transform(report *models.SynodReport) (*TransformResult, error) {
	d := report.Data

	if len(d.Warbands) != 2 {
		return nil, fmt.Errorf("expected 2 warbands, got %d", len(d.Warbands))
	}

	reportDate := time.Unix(d.Date, 0).UTC()
	if reportDate.Year() < 2020 || reportDate.After(time.Now().Add(24*time.Hour)) {
		return nil, fmt.Errorf("unreasonable date: %v", reportDate)
	}

	if d.ScenarioID == "" {
		return nil, fmt.Errorf("empty scenario_id")
	}

	isDraw := d.Winner == nil || *d.Winner == 0
	var winnerID *int
	if !isDraw {
		winnerID = d.Winner
		w0 := d.Warbands[0].WarbandExport.WID
		w1 := d.Warbands[1].WarbandExport.WID
		if *winnerID != w0 && *winnerID != w1 {
			return nil, fmt.Errorf("winner %d doesn't match warbands %d or %d", *winnerID, w0, w1)
		}
	}

	game := models.Game{
		GameReportID:    report.GameReportID,
		ReportDate:      reportDate,
		ScenarioID:      d.ScenarioID,
		IsRanked:        d.Ranked,
		WinnerWarbandID: winnerID,
		IsDraw:          isDraw,
	}

	playerIDs := report.PlayerIDs
	if len(playerIDs) < 2 {
		slog.Warn("report has fewer than 2 player_ids, defaulting to 0", "id", report.GameReportID, "count", len(playerIDs))
		playerIDs = []int{0, 0}
	}

	var participants []models.GameParticipant
	var units []models.GameUnit

	for i, wb := range d.Warbands {
		exp := wb.WarbandExport

		if wb.FactionSlug == "" {
			return nil, fmt.Errorf("empty factionSlug on warband %d", exp.WID)
		}

		allUnits := exp.AllUnits()
		if len(allUnits) == 0 {
			return nil, fmt.Errorf("warband %d has no units", exp.WID)
		}

		baseFaction, _ := db.ParseFaction(wb.FactionSlug)

		// Determine result — winnerID is guaranteed non-nil when !isDraw (checked above)
		var result string
		switch {
		case isDraw:
			result = "draw"
		case *winnerID == exp.WID:
			result = "win"
		default:
			result = "loss"
		}

		wbIDStr := strconv.Itoa(exp.WID)
		kills := d.Kills[wbIDStr]
		vp := d.VP[wbIDStr]

		rosterHash := computeRosterHash(allUnits)

		participants = append(participants, models.GameParticipant{
			GameReportID:    report.GameReportID,
			WarbandID:       exp.WID,
			PlayerID:        playerIDs[i],
			FactionSlug:     wb.FactionSlug,
			BaseFaction:     baseFaction,
			Result:          result,
			Kills:           kills,
			VP:              vp,
			WarbandName:     exp.WNA,
			WarbandDucats:   exp.DR,
			WarbandGlory:    exp.GR,
			RosterHash:      rosterHash,
		})

		for rowIdx, entry := range allUnits {
			eqJSON, err := json.Marshal(entry.Unit.EQ)
			if err != nil {
				return nil, fmt.Errorf("marshaling equipment for unit %d/%d: %w", exp.WID, rowIdx, err)
			}
			advJSON, err := json.Marshal(entry.Unit.ADV)
			if err != nil {
				return nil, fmt.Errorf("marshaling advances for unit %d/%d: %w", exp.WID, rowIdx, err)
			}
			injJSON, err := json.Marshal(entry.Unit.INJ)
			if err != nil {
				return nil, fmt.Errorf("marshaling injuries for unit %d/%d: %w", exp.WID, rowIdx, err)
			}
			upJSON, err := json.Marshal(entry.Unit.UP)
			if err != nil {
				return nil, fmt.Errorf("marshaling upgrades for unit %d/%d: %w", exp.WID, rowIdx, err)
			}

			units = append(units, models.GameUnit{
				GameReportID: report.GameReportID,
				WarbandID:    exp.WID,
				RowIndex:     rowIdx,
				FactionSlug:  wb.FactionSlug,
				BaseFaction:  baseFaction,
				UnitID:       entry.Unit.MDI,
				UnitName:     entry.Unit.MDN,
				UnitType:     entry.Type,
				CostDucats:   entry.Unit.C.D,
				CostGlory:    entry.Unit.C.G,
				Result:       result,
				Equipment:    eqJSON,
				Advances:     advJSON,
				Injuries:     injJSON,
				Upgrades:     upJSON,
			})
		}
	}

	var deeds []models.GameDeed
	for i, deed := range d.Deeds {
		deeds = append(deeds, models.GameDeed{
			GameReportID: report.GameReportID,
			RowIndex:     i,
			DeedID:       deed.ID,
			DeedName:     deed.Name,
			WarbandID:    deed.Warband,
		})
	}

	return &TransformResult{
		Game:         game,
		Participants: participants,
		Units:        units,
		Deeds:        deeds,
	}, nil
}

// computeRosterHash returns SHA-256 of sorted unit model IDs.
func computeRosterHash(units []struct {
	Unit models.SynodUnit
	Type string
}) string {
	ids := make([]string, 0, len(units))
	for _, u := range units {
		if u.Unit.MDI != "" {
			ids = append(ids, u.Unit.MDI)
		}
	}
	sort.Strings(ids)
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
