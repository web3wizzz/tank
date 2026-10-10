// Package limits defines validated local MVP resource bounds and request admission.
package limits

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const SupportedFileBytes int64 = 16 << 20

type Config struct {
	MaxFileBytes      int64
	MaxConcurrent     int
	MaxConnections    int
	MaxPerUser        int
	RequestsPerMinute int
	Burst             int
	RequestTimeout    time.Duration
	MaxHeaderBytes    int
	UserStorageBytes  int64
	TotalStorageBytes int64
	NodeStorageBytes  int64
	NodeFileLimit     int
	NodeConcurrent    int
	NodeConnections   int
}

func Default() Config {
	return Config{MaxFileBytes: SupportedFileBytes, MaxConcurrent: 4, MaxConnections: 64, MaxPerUser: 2,
		RequestsPerMinute: 300, Burst: 60, RequestTimeout: 2 * time.Minute, MaxHeaderBytes: 16 << 10,
		UserStorageBytes: 1 << 30, TotalStorageBytes: 10 << 30, NodeStorageBytes: 10 << 30, NodeConcurrent: 16, NodeConnections: 64, NodeFileLimit: 100000}
}

func Load() (Config, error) {
	c := Default()
	for _, setting := range []struct {
		name             string
		destination      *int64
		minimum, maximum int64
	}{
		{"TANK_MAX_FILE_BYTES", &c.MaxFileBytes, 1, SupportedFileBytes},
		{"TANK_USER_STORAGE_BYTES", &c.UserStorageBytes, 0, 1 << 50},
		{"TANK_TOTAL_STORAGE_BYTES", &c.TotalStorageBytes, 0, 1 << 50},
		{"TANK_NODE_STORAGE_BYTES", &c.NodeStorageBytes, 0, 1 << 50},
	} {
		value, err := integer(setting.name, *setting.destination, setting.minimum, setting.maximum)
		if err != nil {
			return Config{}, err
		}
		*setting.destination = value
	}
	for _, setting := range []struct {
		name             string
		destination      *int
		minimum, maximum int64
	}{
		{"TANK_MAX_CONCURRENT_REQUESTS", &c.MaxConcurrent, 1, 1024},
		{"TANK_MAX_CONNECTIONS", &c.MaxConnections, 1, 4096},
		{"TANK_MAX_CONCURRENT_PER_USER", &c.MaxPerUser, 1, 1024},
		{"TANK_REQUESTS_PER_MINUTE", &c.RequestsPerMinute, 0, 1_000_000},
		{"TANK_REQUEST_BURST", &c.Burst, 1, 1_000_000},
		{"TANK_MAX_HEADER_BYTES", &c.MaxHeaderBytes, 1024, 64 << 10},
		{"TANK_NODE_MAX_CONCURRENT_REQUESTS", &c.NodeConcurrent, 1, 1024},
		{"TANK_NODE_MAX_CONNECTIONS", &c.NodeConnections, 1, 4096},
		{"TANK_NODE_MAX_FILES", &c.NodeFileLimit, 0, 1_000_000_000},
	} {
		value, err := integer(setting.name, int64(*setting.destination), setting.minimum, setting.maximum)
		if err != nil {
			return Config{}, err
		}
		*setting.destination = int(value)
	}
	seconds, err := integer("TANK_REQUEST_TIMEOUT_SECONDS", int64(c.RequestTimeout/time.Second), 1, 300)
	if err != nil {
		return Config{}, err
	}
	c.RequestTimeout = time.Duration(seconds) * time.Second
	return c, c.Validate()
}

func integer(name string, fallback, minimum, maximum int64) (int64, error) {
	raw, exists := os.LookupEnv(name)
	if !exists {
		return fallback, nil
	}
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be an integer between %d and %d", name, minimum, maximum)
	}
	return value, nil
}

func (c Config) Validate() error {
	if c.MaxFileBytes < 1 || c.MaxFileBytes > SupportedFileBytes {
		return fmt.Errorf("invalid maximum file size")
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 1024 || c.MaxPerUser < 1 || c.MaxPerUser > c.MaxConcurrent {
		return fmt.Errorf("per-user concurrency must be positive and no greater than global concurrency")
	}
	if c.RequestsPerMinute < 0 || c.RequestsPerMinute > 1_000_000 || c.Burst < 1 || c.Burst > 1_000_000 {
		return fmt.Errorf("invalid request rate or burst")
	}
	if c.RequestTimeout <= 0 || c.RequestTimeout > 5*time.Minute {
		return fmt.Errorf("request timeout must be positive and at most five minutes")
	}
	if c.MaxHeaderBytes < 1024 || c.MaxHeaderBytes > 64<<10 {
		return fmt.Errorf("invalid maximum header size")
	}
	if c.MaxConnections < 1 || c.MaxConnections > 4096 || c.NodeConnections < 1 || c.NodeConnections > 4096 {
		return fmt.Errorf("invalid connection limit")
	}
	if c.NodeFileLimit < 0 || c.NodeFileLimit > 1_000_000_000 {
		return fmt.Errorf("invalid node file limit")
	}
	if c.NodeConcurrent < 1 || c.NodeConcurrent > 1024 {
		return fmt.Errorf("invalid node concurrency")
	}
	for _, value := range []int64{c.UserStorageBytes, c.TotalStorageBytes, c.NodeStorageBytes} {
		if value < 0 || value > 1<<50 {
			return fmt.Errorf("invalid storage quota")
		}
	}
	return nil
}

type bucket struct {
	tokens  float64
	updated time.Time
	active  int
}

type Governor struct {
	mu     sync.Mutex
	config Config
	active int
	users  map[string]*bucket
	now    func() time.Time
}

func New(config Config) (*Governor, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Governor{config: config, users: make(map[string]*bucket), now: time.Now}, nil
}

// Begin bounds all requests before authentication, so unknown credentials cannot
// bypass the global memory/work limit or create unbounded per-user buckets.
func (g *Governor) Begin() (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active >= g.config.MaxConcurrent {
		return nil, false
	}
	g.active++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); defer g.mu.Unlock(); g.active-- }) }, true
}

func (g *Governor) Timeout() time.Duration { return g.config.RequestTimeout }

// AdmitUser accepts only authenticated canonical principal IDs. It is called
// after Begin and returns an idempotent release function without a waiting queue.
func (g *Governor) AdmitUser(user string) (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	b := g.users[user]
	if b == nil {
		b = &bucket{tokens: float64(g.config.Burst), updated: now}
		g.users[user] = b
	}
	if b.active >= g.config.MaxPerUser {
		return nil, false
	}
	if g.config.RequestsPerMinute > 0 {
		b.tokens = min(float64(g.config.Burst), b.tokens+max(0, now.Sub(b.updated).Minutes())*float64(g.config.RequestsPerMinute))
		b.updated = now
		if b.tokens < 1 {
			return nil, false
		}
		b.tokens--
	}
	b.active++
	var once sync.Once
	return func() { once.Do(func() { g.mu.Lock(); defer g.mu.Unlock(); b.active-- }) }, true
}

func (g *Governor) Admit(user string) (func(), bool) {
	global, ok := g.Begin()
	if !ok {
		return nil, false
	}
	local, ok := g.AdmitUser(user)
	if !ok {
		global()
		return nil, false
	}
	return func() { local(); global() }, true
}
