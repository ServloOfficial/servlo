package backup

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/realrashid/servlo/internal/config"
)

// keyFile is where the one secret that opens every backup lives. It stays on
// the server and never travels with an archive: a key stored beside the
// ciphertext is not encryption, it is compression with extra steps.
const keyFile = "backup.key"

// keyMu serialises generation. The panel is a long-lived process with
// concurrent handlers, and two of them racing to create the key would leave
// one writing archives nobody can open with the key the other kept.
var keyMu sync.Mutex

// KeyPath is where the backup key is stored, which the operator needs in order
// to copy it somewhere safe.
func KeyPath() string { return filepath.Join(config.ConfigDir(), keyFile) }

// Key returns this server's backup key, generating it the first time.
//
// It is generated once and never rotated here. Rotating would leave every
// existing archive unopenable, and doing that as a side effect of a routine
// call is how a backup system quietly stops being one.
func Key() ([]byte, error) {
	keyMu.Lock()
	defer keyMu.Unlock()

	path := KeyPath()
	raw, err := os.ReadFile(path)
	switch {
	case err == nil:
		key, decErr := decodeKey(raw)
		if decErr != nil {
			return nil, fmt.Errorf("%s is not a usable backup key (%w). Every existing backup was written with the "+
				"original, so restore it from wherever you keep it rather than deleting this file", path, decErr)
		}
		return key, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, fmt.Errorf("reading the backup key: %w", err)
	}

	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("generating a backup key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0600); err != nil {
		return nil, fmt.Errorf("writing the backup key: %w", err)
	}
	return key, nil
}

// decodeKey reads the stored form, which is hex so the file can be read out and
// typed back in on the machine a rebuild is happening on.
func decodeKey(raw []byte) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("it is not hex")
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("it is %d bytes, and a key is %d", len(key), KeySize)
	}
	return key, nil
}

// ImportKey writes a key brought from another server, which is the first step
// of a rebuild: the archives are already encrypted with it, so nothing can be
// restored until it is here. An existing key is never overwritten, because
// doing so would strand whatever this server has already backed up.
func ImportKey(encoded string) error {
	keyMu.Lock()
	defer keyMu.Unlock()

	key, err := decodeKey([]byte(encoded))
	if err != nil {
		return fmt.Errorf("that is not a servlo backup key: %w", err)
	}
	path := KeyPath()
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already holds a key. Move it aside first if you are sure, because the backups this "+
			"server has already written can only be opened with it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(hex.EncodeToString(key)+"\n"), 0600)
}
