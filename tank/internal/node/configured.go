package node

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

// ConfiguredClients preserves placement order while optionally assigning one
// private credential to each node. The shared local token remains compatible.
func ConfiguredClients(addresses, sharedToken, credentialFile string) ([]*Client, error) {
	entries := strings.Split(addresses, ",")
	if len(entries) < 3 || len(entries) > 64 {
		return nil, errors.New("configure between 3 and 64 storage nodes")
	}
	var credentials map[string]string
	if credentialFile != "" {
		info, err := os.Lstat(credentialFile)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return nil, errors.New("node credential file must be private regular JSON (mode 0600 or 0400)")
		}
		file, err := os.Open(credentialFile)
		if err != nil {
			return nil, errors.New("could not read node credential file")
		}
		defer file.Close()
		opened, err := file.Stat()
		if err != nil || !os.SameFile(info, opened) || opened.Mode().Perm()&0077 != 0 {
			return nil, errors.New("node credential file changed or is not private")
		}
		data, err := io.ReadAll(io.LimitReader(file, 65537))
		if err != nil || len(data) > 65536 {
			return nil, errors.New("node credential file exceeds supported size")
		}
		if err := json.Unmarshal(data, &credentials); err != nil || len(credentials) != len(entries) {
			return nil, errors.New("node credential file must map each configured URL to one token")
		}
	}
	clients := make([]*Client, 0, len(entries))
	seen := map[string]bool{}
	seenTokens := map[string]bool{}
	for _, entry := range entries {
		address := strings.TrimRight(strings.TrimSpace(entry), "/")
		if seen[address] {
			return nil, errors.New("node URLs must be distinct")
		}
		seen[address] = true
		token := sharedToken
		if credentialFile != "" {
			token = credentials[address]
			if token == "" {
				return nil, errors.New("credential missing for a configured node")
			}
		}
		if credentialFile != "" {
			if seenTokens[token] {
				return nil, errors.New("node credential map requires distinct per-node tokens")
			}
			seenTokens[token] = true
		}
		client, err := NewClient(address, token)
		if err != nil {
			return nil, err
		}
		clients = append(clients, client)
	}
	return clients, nil
}
