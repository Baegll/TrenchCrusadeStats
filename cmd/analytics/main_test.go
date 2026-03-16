package main

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestParseBackfillArgs_Valid(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "198409", "--count", "1000"}
	start, count, err := parseBackfillArgs()
	if err != nil {
		t.Fatal(err)
	}
	if start != 198409 || count != 1000 {
		t.Errorf("got start=%d count=%d, want 198409/1000", start, count)
	}
}

func TestParseBackfillArgs_MissingValue(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for missing --start value")
	}
}

func TestParseBackfillArgs_MissingCountValue(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--count"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for missing --count value")
	}
}

func TestParseBackfillArgs_MissingCount(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "100"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for missing --count")
	}
}

func TestParseBackfillArgs_Negative(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "-5", "--count", "-10"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for negative values")
	}
}

func TestParseBackfillArgs_UnknownFlag(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--foo", "bar"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestParseBackfillArgs_NonInteger(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "abc", "--count", "100"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for non-integer")
	}
}

func TestParseBackfillArgs_NonIntegerCount(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "100", "--count", "xyz"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for non-integer count")
	}
}

func TestParseBackfillArgs_NoArgs(t *testing.T) {
	os.Args = []string{"analytics", "backfill"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for no args (both must be positive)")
	}
}

func TestParseBackfillArgs_ZeroStart(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "0", "--count", "10"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for zero start")
	}
}

func TestParseBackfillArgs_ZeroCount(t *testing.T) {
	os.Args = []string{"analytics", "backfill", "--start", "10", "--count", "0"}
	_, _, err := parseBackfillArgs()
	if err == nil {
		t.Error("expected error for zero count")
	}
}

func TestEnvOrDefault(t *testing.T) {
	os.Setenv("TEST_ENV_KEY", "value")
	defer os.Unsetenv("TEST_ENV_KEY")
	if v := envOrDefault("TEST_ENV_KEY", "fallback"); v != "value" {
		t.Errorf("got %q, want value", v)
	}
	if v := envOrDefault("NONEXISTENT_KEY", "fallback"); v != "fallback" {
		t.Errorf("got %q, want fallback", v)
	}
}

func TestEnvIntOrDefault(t *testing.T) {
	os.Setenv("TEST_INT_KEY", "42")
	defer os.Unsetenv("TEST_INT_KEY")
	if v := envIntOrDefault("TEST_INT_KEY", 0); v != 42 {
		t.Errorf("got %d, want 42", v)
	}
	if v := envIntOrDefault("NONEXISTENT_KEY", 99); v != 99 {
		t.Errorf("got %d, want 99", v)
	}

	os.Setenv("TEST_BAD_INT", "abc")
	defer os.Unsetenv("TEST_BAD_INT")
	if v := envIntOrDefault("TEST_BAD_INT", 5); v != 5 {
		t.Errorf("got %d, want 5 (fallback on bad int)", v)
	}
}

func TestMigrations(t *testing.T) {
	m := migrations()
	if _, ok := m[1]; !ok {
		t.Error("migration 1 should exist")
	}
}

func TestRequireEnv_Set(t *testing.T) {
	os.Setenv("TEST_REQUIRE_ENV", "value")
	defer os.Unsetenv("TEST_REQUIRE_ENV")
	v, err := requireEnv("TEST_REQUIRE_ENV")
	if err != nil {
		t.Fatal(err)
	}
	if v != "value" {
		t.Errorf("got %q, want value", v)
	}
}

func TestRequireEnv_Missing(t *testing.T) {
	_, err := requireEnv("NONEXISTENT_REQUIRED_KEY")
	if err == nil {
		t.Error("expected error for missing required env var")
	}
}

func TestRun_NoArgs(t *testing.T) {
	err := run(context.Background(), nil)
	if err == nil {
		t.Error("expected error for no args")
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"unknown"})
	if err == nil {
		t.Error("expected error for unknown command")
	}
}

func TestRun_Serve(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "")
	os.Setenv("TC_BASE_URL", "http://127.0.0.1:1")
	os.Setenv("ANALYTICS_API_KEY", "test-key")
	os.Setenv("ADMIN_PASSWORD", "test-pass")
	os.Setenv("PORT", "0")
	defer func() {
		os.Unsetenv("DUCKDB_PATH")
		os.Unsetenv("TC_BASE_URL")
		os.Unsetenv("ANALYTICS_API_KEY")
		os.Unsetenv("ADMIN_PASSWORD")
		os.Unsetenv("PORT")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_ = run(ctx, []string{"serve"})
}

func TestRun_Backfill(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "")
	os.Setenv("TC_BASE_URL", "http://127.0.0.1:1")
	os.Setenv("TC_RATE_LIMIT", "1000")
	defer func() {
		os.Unsetenv("DUCKDB_PATH")
		os.Unsetenv("TC_BASE_URL")
		os.Unsetenv("TC_RATE_LIMIT")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	os.Args = []string{"analytics", "backfill", "--start", "1", "--count", "1"}
	_ = run(ctx, []string{"backfill"})
}

func TestRun_Sync(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "")
	os.Setenv("TC_BASE_URL", "http://127.0.0.1:1")
	os.Setenv("TC_RATE_LIMIT", "1000")
	defer func() {
		os.Unsetenv("DUCKDB_PATH")
		os.Unsetenv("TC_BASE_URL")
		os.Unsetenv("TC_RATE_LIMIT")
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = run(ctx, []string{"sync"})
}

func TestRunServe_MissingAPIKey(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "")
	os.Unsetenv("ANALYTICS_API_KEY")
	os.Unsetenv("ADMIN_PASSWORD")
	defer os.Unsetenv("DUCKDB_PATH")

	err := runServe(context.Background())
	if err == nil {
		t.Error("expected error for missing API key")
	}
}

func TestRunServe_MissingAdminPassword(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "")
	os.Setenv("ANALYTICS_API_KEY", "test-key")
	os.Unsetenv("ADMIN_PASSWORD")
	defer func() {
		os.Unsetenv("DUCKDB_PATH")
		os.Unsetenv("ANALYTICS_API_KEY")
	}()

	err := runServe(context.Background())
	if err == nil {
		t.Error("expected error for missing admin password")
	}
}

func TestRunBackfill_BadArgs(t *testing.T) {
	os.Args = []string{"analytics", "backfill"}
	err := runBackfill(context.Background())
	if err == nil {
		t.Error("expected error for missing args")
	}
}

func TestRunBackfill_BadDBPath(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "/nonexistent/dir/db.duckdb")
	defer os.Unsetenv("DUCKDB_PATH")
	os.Args = []string{"analytics", "backfill", "--start", "1", "--count", "1"}
	err := runBackfill(context.Background())
	if err == nil {
		t.Error("expected error for bad DB path")
	}
}

func TestRunSync_BadDBPath(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "/nonexistent/dir/db.duckdb")
	defer os.Unsetenv("DUCKDB_PATH")
	err := runSync(context.Background())
	if err == nil {
		t.Error("expected error for bad DB path")
	}
}

func TestRunServe_BadDBPath(t *testing.T) {
	os.Setenv("DUCKDB_PATH", "/nonexistent/dir/db.duckdb")
	os.Setenv("ANALYTICS_API_KEY", "test-key")
	os.Setenv("ADMIN_PASSWORD", "test-pass")
	defer func() {
		os.Unsetenv("DUCKDB_PATH")
		os.Unsetenv("ANALYTICS_API_KEY")
		os.Unsetenv("ADMIN_PASSWORD")
	}()
	err := runServe(context.Background())
	if err == nil {
		t.Error("expected error for bad DB path")
	}
}
