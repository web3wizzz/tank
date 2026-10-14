package tank

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// MaxPlainBytes reserves the browser envelope's metadata and authentication overhead.
const MaxPlainBytes = MaxFileBytes - 4096 - 41

var envelopeMagic = []byte("TANKENC\x00")

// Recovery is a secret browser-compatible key record. Never log it or send it to Tank.
type Recovery struct {
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	FileID    string `json:"file_id"`
	Key       string `json:"key"`
}

// ParseRecovery imports a bounded version 1 recovery JSON record.
func ParseRecovery(data []byte) (Recovery, error) {
	var r Recovery
	if len(data) > 2048 || json.Unmarshal(data, &r) != nil || r.Version != 1 || r.Algorithm != "AES-256-GCM" || !validFileID(r.FileID) || !validFileID(r.Key) {
		return Recovery{}, errors.New("invalid or unsupported recovery file")
	}
	return r, nil
}

// ExportRecovery returns secret JSON for private local persistence.
func ExportRecovery(r Recovery) ([]byte, error) {
	data, err := json.Marshal(r)
	if err != nil {
		return nil, errors.New("invalid recovery file")
	}
	if _, err = ParseRecovery(data); err != nil {
		return nil, err
	}
	return data, nil
}

func safeEncryptedName(name string) bool {
	if name == "" || name == "." || name == ".." || len(name) > 255 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r == '/' || r == '\\' || r < 32 || (r >= 127 && r <= 159) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			return false
		}
	}
	return true
}

// EncryptFile creates a fresh AES-256-GCM envelope compatible with the browser.
// Its file ID hashes ciphertext. Persist the returned recovery before uploading.
func EncryptFile(data []byte, filename string) ([]byte, Recovery, error) {
	if len(data) < 1 || len(data) > MaxPlainBytes {
		return nil, Recovery{}, ErrInvalidSize
	}
	if !safeEncryptedName(filename) {
		return nil, Recovery{}, errors.New("invalid encrypted filename")
	}
	metadata, _ := json.Marshal(struct {
		Filename string `json:"filename"`
	}{filename})
	plain := make([]byte, 4+len(metadata)+len(data))
	defer clear(plain)
	binary.BigEndian.PutUint32(plain, uint32(len(metadata)))
	copy(plain[4:], metadata)
	copy(plain[4+len(metadata):], data)
	key := make([]byte, 32)
	defer clear(key)
	if _, err := rand.Read(key); err != nil {
		return nil, Recovery{}, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, Recovery{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, Recovery{}, err
	}
	header := make([]byte, 21)
	copy(header, envelopeMagic)
	header[8] = 1
	if _, err = rand.Read(header[9:]); err != nil {
		return nil, Recovery{}, err
	}
	stored := gcm.Seal(header, header[9:], plain, header)
	hash := sha256.Sum256(stored)
	r := Recovery{1, "AES-256-GCM", hex.EncodeToString(hash[:]), hex.EncodeToString(key)}
	return stored, r, nil
}

// DecryptFile authenticates both the ciphertext hash and GCM envelope before returning bytes.
func DecryptFile(stored []byte, recovery Recovery) ([]byte, string, error) {
	if _, err := ExportRecovery(recovery); err != nil {
		return nil, "", err
	}
	if len(stored) < 42 || len(stored) > MaxFileBytes || !bytes.Equal(stored[:8], envelopeMagic) || stored[8] != 1 {
		return nil, "", errors.New("invalid or unsupported encrypted file")
	}
	hash := sha256.Sum256(stored)
	if hex.EncodeToString(hash[:]) != recovery.FileID {
		return nil, "", ErrIntegrity
	}
	key, _ := hex.DecodeString(recovery.Key)
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", err
	}
	plain, err := gcm.Open(nil, stored[9:21], stored[21:], stored[:21])
	if err != nil {
		return nil, "", errors.New("wrong recovery key or damaged encrypted data")
	}
	defer clear(plain)
	if len(plain) < 5 {
		return nil, "", errors.New("invalid encrypted payload")
	}
	size := int(binary.BigEndian.Uint32(plain[:4]))
	if size < 1 || size > 4096 || 4+size >= len(plain) {
		return nil, "", errors.New("invalid encrypted metadata")
	}
	var meta struct {
		Filename string `json:"filename"`
	}
	if !utf8.Valid(plain[4:4+size]) || json.Unmarshal(plain[4:4+size], &meta) != nil || !safeEncryptedName(meta.Filename) {
		return nil, "", errors.New("invalid encrypted filename")
	}
	if len(plain)-4-size > MaxPlainBytes {
		return nil, "", ErrInvalidSize
	}
	return bytes.Clone(plain[4+size:]), meta.Filename, nil
}
