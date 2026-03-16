# Test Plan — Trench Companion Analytics Service

---

## Test-Driven Development Workflow

All new features and bug fixes follow the TDD red-green-refactor cycle:

1. **Red** — Write a failing test that describes the expected behavior
2. **Green** — Write the minimum code to make the test pass
3. **Refactor** — Clean up while keeping tests green

### When to write tests first

- New API endpoint → write handler test asserting response shape + status codes first
- New query function → write integration test with seeded data asserting expected results first
- New transform/validation rule → write table-driven test case first
- Bug fix → write a test that reproduces the bug first, then fix

### When tests can follow

- Pure wiring code (main.go subcommands, env var parsing)
- Refactors that don't change behavior (existing tests cover it)

---

## Test Structure

```
TrenchCompanionStats/
├── cmd/analytics/
│   └── main_test.go                    # CLI arg parsing, env var helpers, run() dispatch
├── internal/
│   ├── api/
│   │   ├── handlers_test.go            # Handler integration tests (httptest + in-memory DB)
│   │   ├── middleware_test.go           # Auth middleware unit tests
│   │   └── ratelimit_test.go           # Rate limiter unit tests
│   ├── db/
│   │   ├── faction_lookup_test.go      # Faction parsing unit tests
│   │   └── queries_test.go             # Query integration tests (in-memory DuckDB)
│   ├── ingestion/
│   │   ├── client_test.go              # HTTP client unit tests (httptest server)
│   │   ├── sync_test.go                # Sync orchestrator integration tests
│   │   ├── transform_test.go           # Transform pipeline unit tests
│   │   └── testdata/
│   │       └── valid_report_raw.json   # Real report fixture from Synod API
│   └── models/
│       └── synod_test.go               # AllUnits unit tests
└── tests/
    └── integration_test.go             # E2E tests: full pipeline, auth, filters
```

**Unit tests** (`internal/*/`) — alongside source, white-box, test internal functions directly.
**Integration/E2E tests** (`tests/`) — black-box, spin up full server + fake Synod API, hit real HTTP endpoints.

---

## Current Status

| Package | Coverage | Tests | Type |
|---|---|---|---|
| `cmd/analytics` | 91.7% | 27 | Unit |
| `internal/api` | 99.2% | 38 | Unit + integration |
| `internal/db` | 83.1% | 44 | Integration (in-memory DuckDB) |
| `internal/ingestion` | 86.0% | 35 | Unit + integration |
| `internal/models` | 100.0% | 2 | Unit |
| `tests/` | — | 5 | E2E |
| **Total** | **89.8%** | **151** | **All passing, no races** |

---

## Test Sections

### 1. Faction Parsing — `internal/db/faction_lookup_test.go`

14 table-driven subtests covering all 6 base factions, `_fv_`/`_fc_` variants, unknown slugs, empty string, prefix collisions.

### 2. Transform Pipeline — `internal/ingestion/transform_test.go`

21 tests: happy path (result assignment, row_index sequencing, unit types, kills/VP extraction), draw via nil and 0, 9 validation failure cases, real report from testdata, few/empty player IDs, second warband winning, no deeds.

### 3. Synod Client — `internal/ingestion/client_test.go`

8 tests using httptest: success, 404, retry on 500, context cancellation, User-Agent header, unexpected status code, all retries exhausted, context cancel during retry.

### 4. Sync Orchestrator — `internal/ingestion/sync_test.go`

17 tests using fake Synod server + in-memory DuckDB: ingestOne, idempotency, backfill with gaps, incremental sync, lock contention, invalid report handling, transform-fail-stores-raw, context cancellation, failed status tracker, processID (404/success/ingest error/fetch error), large batch backfill, rangeEnd clamping, incremental sync context cancel, progress logging.

### 5. DB Queries — `internal/db/queries_test.go`

44 tests using in-memory DuckDB: all query functions with seeded data, empty DB, filters (ranked, date, min_games, scenario, faction, until), insert idempotency, transaction rollback + commit, mutex behavior, helper functions, closed DB error paths, bad migration SQL, bad DB path.

### 6. API Handlers — `internal/api/handlers_test.go`

38 tests using httptest + real router: health (public + with ingestion data), auth enforcement, all 8 stats endpoints (valid + empty DB), invalid params for all endpoints, sync trigger + conflict (409), sync status (no runs + with completed run), empty array verification, parseFilters (all params, invalid ranked_only/since/until, clamping, truncation), filtersToResponse, all handler 500 paths via closed DB, rate limit RemoteAddr fallback, SyncHandlers.Wait.

### 7. Middleware — `internal/api/middleware_test.go`

7 tests: API key auth (valid/invalid/missing), basic auth (valid/invalid/missing), rate limit middleware returning 429.

### 8. Rate Limiter — `internal/api/ratelimit_test.go`

7 tests: burst allowance, over-burst denial, independent keys, token refill after time, eviction logic (manual + goroutine), Close.

### 9. CLI — `cmd/analytics/main_test.go`

27 tests: run() dispatch (no args, unknown command, serve, backfill, sync), parseBackfillArgs (valid, missing values, negative, unknown flags, non-integer, zero start/count, no args), env var helpers, requireEnv (set + missing), runServe (context cancel, missing API key, missing admin password, bad DB path), runBackfill (bad args, bad DB path), runSync (bad DB path), migrations.

### 10. Models — `internal/models/synod_test.go`

2 tests: AllUnits with data (elite + standard + mercenary ordering), AllUnits empty.

### 11. E2E Integration — `tests/integration_test.go`

5 tests with full server + fake Synod API:
- **Full pipeline**: backfill 3 reports → verify all 8 stats endpoints return correct data
- **Incremental sync**: backfill 1 → trigger sync via HTTP → verify new report ingested
- **Auth enforcement**: 7 subtests covering all auth scenarios (no key, bad key, valid key, no auth, bad auth, valid auth, public health)
- **Empty arrays**: all stats endpoints return `[]` not `null` on empty DB
- **Filter params**: valid filters work, invalid params return 400

---

## TDD Checklist for New Features

When adding a new feature, follow this checklist:

```
□ Write failing test(s) in the appropriate test file
□ Run tests — confirm new test fails (red)
□ Implement the minimum code to pass
□ Run tests — confirm all pass (green)
□ Refactor if needed — tests still pass
□ Run full suite: CGO_ENABLED=1 go test ./... -race -count=1
□ Check coverage didn't drop: go test ./... -coverprofile=coverage.out
```

### Example: Adding a new stats endpoint

```
1. Add response type to internal/models/api.go
2. Write query test in internal/db/queries_test.go (RED)
3. Implement query in internal/db/queries.go (GREEN)
4. Write handler test in internal/api/handlers_test.go (RED)
5. Implement handler in internal/api/handlers.go (GREEN)
6. Add route in internal/api/routes.go
7. Add E2E test in tests/integration_test.go
8. Run full suite
```

### Example: Fixing a bug

```
1. Write a test that reproduces the bug (RED)
2. Fix the code (GREEN)
3. Run full suite to verify no regressions
```

---

## Intentionally Uncovered Code

| Function | Why | Coverage Impact |
|---|---|---|
| `main()` | Thin wrapper: logging setup + `run()` + `os.Exit` | ~1% |
| `runServe` shutdown error | Requires `srv.Shutdown` to fail | <1% |
| DB query `rows.Scan`/`rows.Err` paths | Require driver-level failure injection | ~4% |
| `ExecTx` begin/rollback failure | Require driver-level failure injection | <1% |
| `Open` ping/json-extension paths | Require specific DuckDB failure modes | ~2% |
| `ingestOne` individual insert errors | Require mid-transaction DB failures | ~1% |
| `Transform` `json.Marshal` error paths | Cannot fail on `[]SynodItem` structs | ~1% |

### Coverage ceiling

Realistic maximum: **90–92%**. The remaining ~10% is defensive error handling
for infrastructure failures (DB driver errors, TCP read failures, JSON encoding
of valid structs) that cannot be triggered without mocking at the driver level.
This is the correct trade-off — the code handles these errors properly, and
adding mock infrastructure would increase complexity without catching real bugs.

---

## Running Tests

```bash
# All tests with race detector
CGO_ENABLED=1 go test ./... -race -count=1

# Verbose
CGO_ENABLED=1 go test ./... -v -race -count=1

# Unit tests only (fast, no DuckDB)
go test ./internal/ingestion/ -run TestTransform -v

# Integration tests only
CGO_ENABLED=1 go test ./tests/ -v -count=1

# Coverage
CGO_ENABLED=1 go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out
go tool cover -html=coverage.out -o coverage.html
```
