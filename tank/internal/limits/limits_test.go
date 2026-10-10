package limits

import (
	"strconv"
	"testing"
	"time"
)

func defaultEnvironment(t *testing.T) {
	t.Helper()
	for key, value := range map[string]int64{
		"TANK_MAX_FILE_BYTES": 16 << 20, "TANK_USER_STORAGE_BYTES": 1 << 30, "TANK_TOTAL_STORAGE_BYTES": 10 << 30,
		"TANK_NODE_STORAGE_BYTES": 10 << 30, "TANK_MAX_CONCURRENT_REQUESTS": 4, "TANK_MAX_CONNECTIONS": 64, "TANK_NODE_MAX_CONNECTIONS": 64, "TANK_MAX_CONCURRENT_PER_USER": 2,
		"TANK_REQUESTS_PER_MINUTE": 300, "TANK_REQUEST_BURST": 60, "TANK_MAX_HEADER_BYTES": 16 << 10,
		"TANK_NODE_MAX_CONCURRENT_REQUESTS": 16, "TANK_REQUEST_TIMEOUT_SECONDS": 120,
	} {
		t.Setenv(key, strconv.FormatInt(value, 10))
	}
}
func TestConfigurationRejectsUnsupportedValues(t *testing.T) {
	for key, value := range map[string]string{
		"TANK_MAX_FILE_BYTES": "16777217", "TANK_USER_STORAGE_BYTES": "-1", "TANK_TOTAL_STORAGE_BYTES": "not-an-integer",
		"TANK_MAX_CONCURRENT_REQUESTS": "0", "TANK_REQUESTS_PER_MINUTE": "-1", "TANK_REQUEST_BURST": "0",
		"TANK_NODE_MAX_CONCURRENT_REQUESTS": "1025", "TANK_REQUEST_TIMEOUT_SECONDS": "301", "TANK_MAX_HEADER_BYTES": "1",
	} {
		t.Run(key, func(t *testing.T) {
			defaultEnvironment(t)
			t.Setenv(key, value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid setting accepted")
			}
		})
	}
	defaultEnvironment(t)
	t.Setenv("TANK_MAX_CONCURRENT_PER_USER", "5")
	if _, err := Load(); err == nil {
		t.Fatal("per-user bound above global bound accepted")
	}
}
func TestConfigurationAllowsExplicitQuotaAndRateDisable(t *testing.T) {
	defaultEnvironment(t)
	for _, key := range []string{"TANK_USER_STORAGE_BYTES", "TANK_TOTAL_STORAGE_BYTES", "TANK_NODE_STORAGE_BYTES", "TANK_REQUESTS_PER_MINUTE"} {
		t.Setenv(key, "0")
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.UserStorageBytes != 0 || c.TotalStorageBytes != 0 || c.NodeStorageBytes != 0 || c.RequestsPerMinute != 0 {
		t.Fatal("disable settings not respected")
	}
}
func TestGovernorBoundsGlobalAndPerUserWorkAndReleasesOnce(t *testing.T) {
	c := Default()
	c.MaxConcurrent = 2
	c.MaxPerUser = 1
	c.RequestsPerMinute = 0
	g, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	alice, ok := g.Admit("Alice")
	if !ok {
		t.Fatal("first request rejected")
	}
	if _, ok := g.Admit("Alice"); ok {
		t.Fatal("per-user saturation accepted")
	}
	bob, ok := g.Admit("Bob")
	if !ok {
		t.Fatal("one user blocked another user's free slot")
	}
	if _, ok := g.Begin(); ok {
		t.Fatal("global saturation accepted before authentication")
	}
	alice()
	alice()
	bob()
	bob()
	again, ok := g.Admit("Alice")
	if !ok {
		t.Fatal("idempotent release leaked capacity")
	}
	again()
}
func TestRateBudgetIsPerPrincipalAndRefills(t *testing.T) {
	c := Default()
	c.RequestsPerMinute = 60
	c.Burst = 1
	g, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_800_000_000, 0)
	g.now = func() time.Time { return now }
	release, ok := g.Admit("Alice")
	if !ok {
		t.Fatal("first request rejected")
	}
	release()
	if _, ok := g.Admit("Alice"); ok {
		t.Fatal("rate exhaustion accepted")
	}
	release, ok = g.Admit("Bob")
	if !ok {
		t.Fatal("rate budget leaked across users")
	}
	release()
	now = now.Add(time.Second)
	release, ok = g.Admit("Alice")
	if !ok {
		t.Fatal("rate budget did not refill")
	}
	release()
}
