package tank

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os/exec"
	"testing"
)

func TestEncryptionRoundTripAndRejection(t *testing.T) {
	input := []byte("Private PDF bytes — Tank")
	stored, r, err := EncryptFile(input, "Résumé.pdf")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := ExportRecovery(r)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(encoded)
	imported, err := ParseRecovery(encoded)
	if err != nil {
		t.Fatal(err)
	}
	data, name, err := DecryptFile(stored, imported)
	if err != nil || !bytes.Equal(input, data) || name != "Résumé.pdf" {
		t.Fatal("round trip failed")
	}
	other, otherR, err := EncryptFile(input, "Résumé.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(stored, other) || r.FileID == otherR.FileID || r.Key == otherR.Key {
		t.Fatal("encryption must use fresh randomness")
	}
	if _, _, err = DecryptFile(stored, otherR); err == nil {
		t.Fatal("wrong file accepted")
	}
	wrong := r
	wrong.Key = otherR.Key
	if _, _, err = DecryptFile(stored, wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
	for _, offset := range []int{9, 21, len(stored) - 1} {
		altered := bytes.Clone(stored)
		altered[offset] ^= 1
		sum := sha256.Sum256(altered)
		record := r
		record.FileID = hex.EncodeToString(sum[:])
		if _, _, err = DecryptFile(altered, record); err == nil {
			t.Fatal("tampering accepted")
		}
	}
	for _, name := range []string{"../a", "a\\b", "bad\n", ".", "..", string([]byte{0xff})} {
		if _, _, err = EncryptFile(input, name); err == nil {
			t.Fatal("unsafe name accepted")
		}
	}
	for _, bad := range [][]byte{nil, []byte("null"), []byte("{}"), bytes.Repeat([]byte("x"), 2049)} {
		if _, err = ParseRecovery(bad); err == nil {
			t.Fatal("invalid recovery accepted")
		}
	}
	if _, _, err = EncryptFile(nil, "a"); err == nil {
		t.Fatal("empty file accepted")
	}
	if _, _, err = EncryptFile(make([]byte, MaxPlainBytes+1), "a"); err == nil {
		t.Fatal("oversize accepted")
	}
	stored, r, err = EncryptFile(make([]byte, MaxPlainBytes), "a")
	if err != nil || len(stored) > MaxFileBytes {
		t.Fatal("maximum file failed")
	}
	if data, _, err = DecryptFile(stored, r); err != nil || len(data) != MaxPlainBytes {
		t.Fatal("maximum decrypt failed")
	}
}

// The interoperability gate requires Node 24 alongside Go and exercises the actual
// browser implementation rather than a separately reimplemented test cipher.
func TestBrowserAndTypeScriptCompatibility(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("Node 24 required for cross-language compatibility")
	}
	input := []byte("Interoperability bytes — \x00\xff")
	stored, r, err := EncryptFile(input, "Résumé.pdf")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(struct {
		Bytes    []byte   `json:"bytes"`
		Recovery Recovery `json:"recovery"`
	}{stored, r})
	defer clear(payload)
	script := `
 import * as browser from '../../frontend/src/lib/file-crypto.ts';
 import * as sdk from '../../sdk-ts/src/encryption.ts';
 let text=''; for await (const chunk of process.stdin) text+=chunk;
 const input=JSON.parse(text);
 const stored=Uint8Array.from(Buffer.from(input.bytes,'base64')).buffer;
 const a=await browser.decryptFile(stored,browser.parseRecovery(JSON.stringify(input.recovery)));
 const b=await sdk.decryptFile(stored,sdk.parseRecovery(sdk.exportRecovery(input.recovery)));
 if(a.filename!==b.filename || Buffer.compare(Buffer.from(a.bytes),Buffer.from(b.bytes))) throw new Error('Go interoperability failed');
 const fromBrowser=await browser.encryptFile(a.bytes,a.filename);
 const fromSDK=await sdk.encryptFile(b.bytes,b.filename);
 for(const [source,target] of [[fromBrowser,sdk],[fromSDK,browser]]) {
  const result=await target.decryptFile(source.bytes,source.recovery);
  if(Buffer.compare(Buffer.from(result.bytes),Buffer.from(a.bytes))) throw new Error('TS/browser interoperability failed');
 }
 process.stdout.write(JSON.stringify([fromBrowser,fromSDK].map(x=>({bytes:Buffer.from(x.bytes).toString('base64'),recovery:x.recovery}))));
 `
	cmd := exec.Command("node", "--input-type=module", "-e", script)
	cmd.Stdin = bytes.NewReader(payload)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal("Node compatibility process failed:", err)
	}
	defer clear(output)
	var results []struct {
		Bytes    []byte   `json:"bytes"`
		Recovery Recovery `json:"recovery"`
	}
	if err = json.Unmarshal(output, &results); err != nil || len(results) != 2 {
		t.Fatal("invalid interoperability result")
	}
	for _, result := range results {
		data, name, err := DecryptFile(result.Bytes, result.Recovery)
		if err != nil || !bytes.Equal(data, input) || name != "Résumé.pdf" {
			t.Fatal("browser/TypeScript to Go compatibility failed")
		}
	}
}
