package models

// SynodReport is the raw response from GET /wp-json/synod/v1/game-report/{id}.
type SynodReport struct {
	GameReportID int             `json:"game_report_id"`
	PlayerIDs    []int           `json:"player_ids"`
	WarbandIDs   []int           `json:"warband_ids"`
	Data         SynodReportData `json:"data"`
}

// SynodReportData contains the game details nested under "data" in the API response.
type SynodReportData struct {
	Winner     *int           `json:"winner"` // nil = draw
	Kills      map[string]int `json:"kills"`  // keyed by warband ID string
	VP         map[string]int `json:"vp"`
	ScenarioID string         `json:"scenario_id"`
	Date       int64          `json:"date"`
	Ranked     bool           `json:"ranked"`
	Deeds      []SynodDeed    `json:"deeds"`
	Warbands   []SynodWarband `json:"warbands"`
}

// SynodWarband represents a warband entry in the game report.
type SynodWarband struct {
	FactionSlug   string      `json:"factionSlug"`
	WarbandExport SynodExport `json:"warbandExport"`
}

// SynodExport is the full warband snapshot at report submission time.
type SynodExport struct {
	WID int         `json:"wid"` // warband ID
	WNA string      `json:"wna"` // warband name
	DR  int         `json:"dr"`  // ducats (total warband cost)
	GR  int         `json:"gr"`  // glory (campaign progression)
	ELT []SynodUnit `json:"elt"` // elite units (leaders, heroes)
	MDS []SynodUnit `json:"mds"` // standard units (troops)
	MRC []SynodUnit `json:"mrc"` // mercenary units
}

// AllUnits returns all units concatenated in canonical order: elite, standard, mercenary.
func (e SynodExport) AllUnits() []struct {
	Unit SynodUnit
	Type string
} {
	out := make([]struct {
		Unit SynodUnit
		Type string
	}, 0, len(e.ELT)+len(e.MDS)+len(e.MRC))
	for _, u := range e.ELT {
		out = append(out, struct {
			Unit SynodUnit
			Type string
		}{u, "elite"})
	}
	for _, u := range e.MDS {
		out = append(out, struct {
			Unit SynodUnit
			Type string
		}{u, "standard"})
	}
	for _, u := range e.MRC {
		out = append(out, struct {
			Unit SynodUnit
			Type string
		}{u, "mercenary"})
	}
	return out
}

// SynodUnit represents a single unit in a warband export.
type SynodUnit struct {
	MDN string      `json:"mdn"` // model display name
	MDI string      `json:"mdi"` // model ID (e.g. "md_heretictrooper")
	C   SynodCost   `json:"c"`   // cost in ducats/glory
	EQ  []SynodItem `json:"eq"`  // equipment
	UP  []SynodItem `json:"up"`  // upgrades
	ADV []SynodItem `json:"adv"` // advances/skills
	INJ []SynodItem `json:"inj"` // injuries
}

// SynodCost is the ducat/glory cost of a unit.
type SynodCost struct {
	D int `json:"d"` // ducats
	G int `json:"g"` // glory
}

// SynodItem is a named+ID'd item (equipment, upgrade, advance, or injury).
type SynodItem struct {
	N string `json:"n"` // name
	I string `json:"i"` // id
}

// SynodDeed is a game achievement from the deeds array.
type SynodDeed struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Warband *int   `json:"warband"` // nil if not attributed to a warband
}

// SynodError is the 404 error response shape from the Synod API.
type SynodError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
