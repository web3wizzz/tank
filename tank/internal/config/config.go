package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"tank.local/tank/internal/limits"
)

type Config struct {
	Limits          limits.Config
	CoordinatorAddr string
	NodeAddr        string
	NodeDataDir     string
	DatabasePath    string
	MaxSegmentBytes int
}

func Load() (Config, error) {
	resourceLimits, err := limits.Load()
	if err != nil {
		return Config{}, err
	}
	c := Config{
		Limits:          resourceLimits,
		CoordinatorAddr: value("TANK_COORDINATOR_ADDR", "127.0.0.1:8080"),
		NodeAddr:        value("TANK_NODE_ADDR", "127.0.0.1:9101"),
		NodeDataDir:     value("TANK_NODE_DATA_DIR", "data/node1"),
		DatabasePath:    value("TANK_DATABASE_PATH", "data/tank.sqlite"),
		MaxSegmentBytes: 4 << 20,
	}

	if raw, ok := os.LookupEnv("TANK_MAX_SEGMENT_BYTES"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return Config{}, fmt.Errorf("TANK_MAX_SEGMENT_BYTES must be an integer")
		}
		c.MaxSegmentBytes = n
	}

	for name, addr := range map[string]string{
		"TANK_COORDINATOR_ADDR": c.CoordinatorAddr,
		"TANK_NODE_ADDR":        c.NodeAddr,
	} {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || host == "" {
			return Config{}, fmt.Errorf("%s must be an explicit host:port", name)
		}
		p, err := strconv.Atoi(port)
		if err != nil || p < 1 || p > 65535 {
			return Config{}, fmt.Errorf("%s has an invalid port", name)
		}
	}

	if c.MaxSegmentBytes < 1 || c.MaxSegmentBytes > 16<<20 {
		return Config{}, fmt.Errorf("segment size must be between 1 byte and 16 MiB")
	}
	return c, nil
}

func value(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
