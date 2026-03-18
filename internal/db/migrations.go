package db

// Migration001 is the initial schema DDL.
// Version is recorded by the migration runner AFTER this DDL succeeds.
const Migration001 = `
CREATE TABLE raw_game_reports (
    game_report_id      INTEGER PRIMARY KEY,
    ingested_at         TIMESTAMP DEFAULT current_timestamp,
    raw_json            JSON NOT NULL
);

CREATE TABLE games (
    game_report_id      INTEGER PRIMARY KEY,
    report_date         TIMESTAMP NOT NULL,
    scenario_id         VARCHAR NOT NULL,
    is_ranked           BOOLEAN NOT NULL,
    winner_warband_id   INTEGER,
    is_draw             BOOLEAN NOT NULL DEFAULT FALSE,
    FOREIGN KEY (game_report_id) REFERENCES raw_game_reports(game_report_id)
);

CREATE TABLE game_participants (
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
    warband_snapshot    JSON NOT NULL,
    PRIMARY KEY (game_report_id, warband_id),
    FOREIGN KEY (game_report_id) REFERENCES games(game_report_id)
);

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
    PRIMARY KEY (game_report_id, warband_id, row_index),
    FOREIGN KEY (game_report_id, warband_id) REFERENCES game_participants(game_report_id, warband_id)
);

CREATE TABLE game_deeds (
    game_report_id      INTEGER NOT NULL,
    row_index           INTEGER NOT NULL,
    deed_id             VARCHAR NOT NULL,
    deed_name           VARCHAR NOT NULL,
    warband_id          INTEGER,
    PRIMARY KEY (game_report_id, row_index),
    FOREIGN KEY (game_report_id) REFERENCES games(game_report_id)
);

CREATE TABLE faction_lookup (
    faction_slug        VARCHAR PRIMARY KEY,
    base_faction        VARCHAR NOT NULL,
    display_name        VARCHAR NOT NULL,
    is_variant          BOOLEAN NOT NULL
);

CREATE SEQUENCE ingestion_log_seq START 1;

CREATE TABLE ingestion_log (
    id                  INTEGER PRIMARY KEY DEFAULT nextval('ingestion_log_seq'),
    run_at              TIMESTAMP DEFAULT current_timestamp,
    completed_at        TIMESTAMP,
    status              VARCHAR NOT NULL DEFAULT 'running' CHECK (status IN ('running', 'completed', 'failed')),
    run_type            VARCHAR NOT NULL CHECK (run_type IN ('backfill', 'incremental')),
    id_range_start      INTEGER,
    id_range_end        INTEGER,
    reports_found       INTEGER NOT NULL DEFAULT 0,
    reports_inserted    INTEGER NOT NULL DEFAULT 0,
    ids_scanned         INTEGER NOT NULL DEFAULT 0,
    errors              INTEGER NOT NULL DEFAULT 0,
    duration_ms         INTEGER,
    notes               VARCHAR
);
`

// Migration002 drops the raw_json and warband_snapshot columns to reduce memory footprint.
// raw_json: write-only, never queried, data re-fetchable from Synod API.
// warband_snapshot: write-only, never queried, data already normalized into game_units.
// DuckDB requires recreating tables when foreign keys prevent column drops.
const Migration002 = `
-- 1. Recreate game_participants without warband_snapshot (drop FK from game_units first)
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

-- 2. Recreate game_units (FK to game_participants was dropped above)
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

-- 3. Recreate raw_game_reports without raw_json (drop FK from games first)
CREATE TABLE raw_game_reports_new (
    game_report_id      INTEGER PRIMARY KEY,
    ingested_at         TIMESTAMP DEFAULT current_timestamp
);
INSERT INTO raw_game_reports_new SELECT game_report_id, ingested_at FROM raw_game_reports;
DROP TABLE game_deeds;
DROP TABLE games;
DROP TABLE raw_game_reports;
ALTER TABLE raw_game_reports_new RENAME TO raw_game_reports;

-- 4. Recreate games without FK to raw_game_reports
CREATE TABLE games (
    game_report_id      INTEGER PRIMARY KEY,
    report_date         TIMESTAMP NOT NULL,
    scenario_id         VARCHAR NOT NULL,
    is_ranked           BOOLEAN NOT NULL,
    winner_warband_id   INTEGER,
    is_draw             BOOLEAN NOT NULL DEFAULT FALSE
);

-- 5. Recreate game_deeds
CREATE TABLE game_deeds (
    game_report_id      INTEGER NOT NULL,
    row_index           INTEGER NOT NULL,
    deed_id             VARCHAR NOT NULL,
    deed_name           VARCHAR NOT NULL,
    warband_id          INTEGER,
    PRIMARY KEY (game_report_id, row_index)
);
`
