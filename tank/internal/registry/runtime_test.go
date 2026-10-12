package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWorkerRuntimeMustMatchExactly(t *testing.T) {
	fixture := fixtureReader(t)
	server := httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
	defer server.Close()
	r, err := openTestnetReader(context.Background(), arcReaderConfig(server.URL), server.Client())
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.VerifyRuntime(context.Background(), []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := r.VerifyRuntime(context.Background(), []byte{2}); err == nil {
		t.Fatal("incorrect registry runtime accepted")
	}
	fixture.chainID = "0x13b2" // Arc Mainnet 5042.
	if err := r.VerifyRuntime(context.Background(), []byte{1}); err == nil {
		t.Fatal("mainnet runtime accepted")
	}
}
