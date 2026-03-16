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
