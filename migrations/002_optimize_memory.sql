-- Migration 002: Drop raw_json and warband_snapshot columns
-- Reduces memory footprint by ~50MB. Both columns were write-only.
-- raw_json: data re-fetchable from Synod API
-- warband_snapshot: data already normalized into game_units
-- Foreign keys removed (unnecessary for analytics workload, caused DuckDB migration issues).

-- Step 1: Recreate game_participants without warband_snapshot
CREATE TABLE game_participants_new (
    game_report_id      INTEGER NOT NULL,
    warband_id          INTEGER NOT NULL,
    player_id           INTEGER NOT NULL,
    faction_slug        VARCHAR NOT NULL,
    base_faction        VARCHAR NOT NULL,
    result              VARCHAR NOT NULL CHECK (result IN ('win', 'loss', 'draw')),
    kills               INTEGER NOT NULL DEFAULT 0,
    vp                  INTEGER NOT NULL DEFAULT 0,
    warband_name        VARCHAR,
    warband_ducats      INTEGER NOT NULL DEFAULT 0,
    warband_glory       INTEGER NOT NULL DEFAULT 0,
    roster_hash         VARCHAR NOT NULL,
    PRIMARY KEY (game_report_id, warband_id)
);
INSERT INTO game_participants_new SELECT game_report_id, warband_id, player_id, faction_slug, base_faction, result, kills, vp, warband_name, warband_ducats, warband_glory, roster_hash FROM game_participants;
DROP TABLE game_units;
DROP TABLE game_participants;
ALTER TABLE game_participants_new RENAME TO game_participants;

-- Step 2: Recreate game_units (dropped above to remove FK)
CREATE TABLE game_units (
    game_report_id      INTEGER NOT NULL,
    warband_id          INTEGER NOT NULL,
    row_index           INTEGER NOT NULL,
    faction_slug        VARCHAR NOT NULL,
    base_faction        VARCHAR NOT NULL,
    unit_id             VARCHAR NOT NULL,
    unit_name           VARCHAR NOT NULL,
    unit_type           VARCHAR NOT NULL CHECK (unit_type IN ('elite', 'standard', 'mercenary')),
    cost_ducats         INTEGER NOT NULL DEFAULT 0,
    cost_glory          INTEGER NOT NULL DEFAULT 0,
    result              VARCHAR NOT NULL CHECK (result IN ('win', 'loss', 'draw')),
    equipment           JSON,
    advances            JSON,
    injuries            JSON,
    upgrades            JSON,
    PRIMARY KEY (game_report_id, warband_id, row_index)
);

-- Step 3: Recreate raw_game_reports without raw_json
CREATE TABLE raw_game_reports_new (
    game_report_id      INTEGER PRIMARY KEY,
    ingested_at         TIMESTAMP DEFAULT current_timestamp
);
INSERT INTO raw_game_reports_new SELECT game_report_id, ingested_at FROM raw_game_reports;
DROP TABLE game_deeds;
DROP TABLE games;
DROP TABLE raw_game_reports;
ALTER TABLE raw_game_reports_new RENAME TO raw_game_reports;

-- Step 4: Recreate games without FK to raw_game_reports
CREATE TABLE games (
    game_report_id      INTEGER PRIMARY KEY,
    report_date         TIMESTAMP NOT NULL,
    scenario_id         VARCHAR NOT NULL,
    is_ranked           BOOLEAN NOT NULL,
    winner_warband_id   INTEGER,
    is_draw             BOOLEAN NOT NULL DEFAULT FALSE
);

-- Step 5: Recreate game_deeds
CREATE TABLE game_deeds (
    game_report_id      INTEGER NOT NULL,
    row_index           INTEGER NOT NULL,
    deed_id             VARCHAR NOT NULL,
    deed_name           VARCHAR NOT NULL,
    warband_id          INTEGER,
    PRIMARY KEY (game_report_id, row_index)
);

-- Resulting schema after migration 002:
--
-- raw_game_reports: game_report_id (PK), ingested_at
-- games: game_report_id (PK), report_date, scenario_id, is_ranked, winner_warband_id, is_draw
-- game_participants: (game_report_id, warband_id) PK, player_id, faction_slug, base_faction, result, kills, vp, warband_name, warband_ducats, warband_glory, roster_hash
-- game_units: (game_report_id, warband_id, row_index) PK, faction_slug, base_faction, unit_id, unit_name, unit_type, cost_ducats, cost_glory, result, equipment, advances, injuries, upgrades
-- game_deeds: (game_report_id, row_index) PK, deed_id, deed_name, warband_id
-- faction_lookup: faction_slug (PK), base_faction, display_name, is_variant
-- ingestion_log: id (PK), run_at, completed_at, status, run_type, id_range_start, id_range_end, reports_found, reports_inserted, ids_scanned, errors, duration_ms, notes
