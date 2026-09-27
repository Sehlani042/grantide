package gateway

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Store struct {
	dir  string
	aead cipher.AEAD
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func newID(prefix string) string { return prefix + "_" + randomToken()[:16] }
func tokenHash(token string) string {
	s := sha256.Sum256([]byte(token))
	return hex.EncodeToString(s[:])
}

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	keyPath := filepath.Join(dir, "master.key")
	key, err := os.ReadFile(keyPath)
	if errors.Is(err, os.ErrNotExist) {
		if _, stateErr := os.Stat(filepath.Join(dir, "state.enc")); !errors.Is(stateErr, os.ErrNotExist) {
			return nil, errors.New("state exists but master.key is missing; restore the key backup")
		}
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		err = writeExclusive(keyPath, key)
	}
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("invalid master key length")
	}
	if err := os.Chmod(keyPath, 0600); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Store{dir: dir, aead: aead}, nil
}

func writeExclusive(path string, b []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func (s *Store) Load() (State, error) {
	state := State{Schema: 1, Revision: 1, Agents: []Agent{}, Services: []Service{}, Rules: []Rule{}, Audit: []Event{}}
	b, err := os.ReadFile(filepath.Join(s.dir, "state.enc"))
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	n := s.aead.NonceSize()
	if len(b) < n {
		return state, errors.New("encrypted state is truncated")
	}
	plain, err := s.aead.Open(nil, b[:n], b[n:], []byte("grantide-state-v1"))
	if err != nil {
		return state, errors.New("state authentication failed; restore a valid backup")
	}
	if err = json.Unmarshal(plain, &state); err != nil {
		return state, err
	}
	if state.Schema != 1 {
		return state, fmt.Errorf("unsupported schema %d", state.Schema)
	}
	return state, nil
}

func (s *Store) Save(state State) error {
	b, err := json.Marshal(state)
	if err != nil {
		return err
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	encrypted := s.aead.Seal(nonce, nonce, b, []byte("grantide-state-v1"))
	f, err := os.CreateTemp(s.dir, ".state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(encrypted); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(name, filepath.Join(s.dir, "state.enc"))
}

func OperatorToken(dir string) (string, error) {
	path := filepath.Join(dir, "operator.token")
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		token := randomToken()
		if err = writeExclusive(path, []byte(token)); err != nil {
			return "", err
		}
		return token, nil
	}
	if err != nil {
		return "", err
	}
	if len(b) != 64 {
		return "", errors.New("invalid operator token")
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", err
	}
	return string(b), nil
}
