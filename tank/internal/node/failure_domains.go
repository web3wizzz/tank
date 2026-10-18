package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"unicode"
)

// FailureDomain is a placement boundary, not proof of independent infrastructure.
// Remote endpoints default to their canonical hostname. Loopback defaults retain
// endpoint identity solely for explicitly simulated local multi-node fixtures.
func (c *Client) FailureDomain() string {
	if c.failureDomain != "" {
		return c.failureDomain
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return ""
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	ip := net.ParseIP(host)
	if host == "localhost" || (ip != nil && ip.IsLoopback()) {
		return "local-simulation:" + c.BaseURL
	}
	if ip != nil {
		host = ip.String()
	}
	return "host:" + host
}

// ConfigureFailureDomains validates a complete exact-URL mapping before changing
// clients. Call once at startup, before sharing clients with concurrent workers.
// Operators must map endpoints sharing a machine/storage failure to one label.
func ConfigureFailureDomains(clients []*Client, file string, required bool) error {
	fail := errors.New("invalid or incomplete node failure-domain configuration")
	if file == "" {
		if required {
			return fail
		}
		return nil
	}
	info, err := os.Lstat(file)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return fail
	}
	fd, err := syscall.Open(file, syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		return fail
	}
	f := os.NewFile(uintptr(fd), file)
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return fail
	}
	body, err := io.ReadAll(io.LimitReader(f, 65537))
	if err != nil || len(body) > 65536 {
		return fail
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fail
	}
	mapping := map[string]string{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return fail
		}
		address, ok := token.(string)
		if !ok {
			return fail
		}
		if _, duplicate := mapping[address]; duplicate {
			return fail
		}
		var label string
		if decoder.Decode(&label) != nil || label == "" || label != strings.TrimSpace(label) || len(label) > 128 {
			return fail
		}
		for _, r := range label {
			if unicode.IsControl(r) {
				return fail
			}
		}
		mapping[address] = label
	}
	if _, err = decoder.Token(); err != nil {
		return fail
	}
	if _, err = decoder.Token(); err != io.EOF {
		return fail
	}
	if len(mapping) != len(clients) {
		return fail
	}
	seen := map[string]bool{}
	for _, client := range clients {
		if client == nil || seen[client.BaseURL] || mapping[client.BaseURL] == "" {
			return fail
		}
		seen[client.BaseURL] = true
	}
	for _, client := range clients {
		client.failureDomain = mapping[client.BaseURL]
	}
	return nil
}
