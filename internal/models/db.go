package models

import (
	"encoding/json"
	"time"
)

// Game represents a row in the games table.
type Game struct {
	GameReportID    int
	ReportDate      time.Time
	ScenarioID      string
	IsRanked        bool
	WinnerWarbandID *int
	IsDraw          bool
}

// GameParticipant represents a row in the game_participants table.
type GameParticipant struct {
	GameReportID    int
	WarbandID       int
	PlayerID        int
	FactionSlug     string
	BaseFaction     string
	Result          string // "win", "loss", "draw"
	Kills           int
	VP              int
	WarbandName     string
	WarbandDucats   int
	WarbandGlory    int
	RosterHash      string
	WarbandSnapshot json.RawMessage
}

// GameUnit represents a row in the game_units table.
type GameUnit struct {
	GameReportID int
	WarbandID    int
	RowIndex     int
	FactionSlug  string
	BaseFaction  string
	UnitID       string
	UnitName     string
	UnitType     string // "elite", "standard", "mercenary"
	CostDucats   int
	CostGlory    int
	Result       string
	Equipment    json.RawMessage
	Advances     json.RawMessage
	Injuries     json.RawMessage
	Upgrades     json.RawMessage
}

// GameDeed represents a row in the game_deeds table.
type GameDeed struct {
	GameReportID int
	RowIndex     int
	DeedID       string
	DeedName     string
	WarbandID    *int
}

// Faction represents a row in the faction_lookup table.
type Faction struct {
	FactionSlug string
	BaseFaction string
	DisplayName string
	IsVariant   bool
}

// IngestionRun represents a row in the ingestion_log table.
type IngestionRun struct {
	ID              int
	RunAt           time.Time
	CompletedAt     *time.Time
	Status          string // "running", "completed", "failed"
	RunType         string // "backfill", "incremental"
	IDRangeStart    *int
	IDRangeEnd      *int
	ReportsFound    int
	ReportsInserted int
	IDsScanned      int
	Errors          int
	DurationMs      *int
	Notes           string
}
