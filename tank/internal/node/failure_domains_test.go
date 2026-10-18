package node

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailureDomainDefaultsAndExactConfiguration(t *testing.T) {
	makeClient := func(address string) *Client {
		c, err := NewClient(address, strings.Repeat("x", 32))
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	a, b := makeClient("https://STORAGE.example:9101"), makeClient("https://storage.example.:9102")
	if a.FailureDomain() != b.FailureDomain() {
		t.Fatal("ports/case/trailing dot bypass hostname domain")
	}
	localA, localB := makeClient("http://127.0.0.1:9101"), makeClient("http://127.0.0.1:9102")
	if localA.FailureDomain() == localB.FailureDomain() {
		t.Fatal("simulated local endpoints collapsed")
	}
	clients := []*Client{a, b}
	before := a.FailureDomain()
	if ConfigureFailureDomains(clients, "", true) == nil {
		t.Fatal("required config omitted")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "domains.json")
	cases := []string{
		`{}`, `{"https://STORAGE.example:9101":"a"}`,
		`{"https://STORAGE.example:9101":"a","https://storage.example.:9102":"b","extra":"c"}`,
		`{"https://STORAGE.example:9101":"a","https://storage.example.:9102":" "}`,
		`{"https://STORAGE.example:9101":"a","https://STORAGE.example:9101":"b"}`,
		`{"https://STORAGE.example:9101":"a","https://storage.example.:9102":null}`,
		`{"https://STORAGE.example:9101":"a","https://storage.example.:9102":"b"} {}`,
		strings.Repeat(" ", 65537),
	}
	for _, body := range cases {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if ConfigureFailureDomains(clients, path, true) == nil {
			t.Fatal("invalid mapping accepted")
		}
		if a.FailureDomain() != before {
			t.Fatal("invalid mapping partially mutated clients")
		}
	}
	body := `{"https://STORAGE.example:9101":"rack-a","https://storage.example.:9102":"rack-a"}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if ConfigureFailureDomains(clients, alias, true) == nil {
		t.Fatal("symlink accepted")
	}
	if err := ConfigureFailureDomains(clients, path, true); err != nil {
		t.Fatal(err)
	}
	if a.FailureDomain() != "rack-a" || b.FailureDomain() != "rack-a" {
		t.Fatal("explicit map not applied")
	}
}
