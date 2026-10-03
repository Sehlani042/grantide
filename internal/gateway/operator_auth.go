package gateway

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const operatorCookie = "grantide_console"
const passwordIterations = 600000

type operatorAuthRecord struct {
	Schema   int                  `json:"schema"`
	Salt     []byte               `json:"salt,omitempty"`
	Digest   []byte               `json:"digest,omitempty"`
	Sessions map[string]time.Time `json:"sessions"`
}

type OperatorAuth struct {
	mu       sync.Mutex
	path     string
	record   operatorAuthRecord
	failures []time.Time
	now      func() time.Time
}

var errOperatorCredentials = errors.New("Invalid administrator password")
var errOperatorThrottle = errors.New("Too many login attempts; try again in one minute")

func OpenOperatorAuth(dir string) (*OperatorAuth, error) {
	a := &OperatorAuth{path: filepath.Join(dir, "operator-auth.json"), now: time.Now,
		record: operatorAuthRecord{Schema: 1, Sessions: make(map[string]time.Time)}}
	b, err := os.ReadFile(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return a, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(b, &a.record) != nil || a.record.Schema != 1 || len(a.record.Sessions) > 32 ||
		(len(a.record.Digest) != 0 && (len(a.record.Digest) != 32 || len(a.record.Salt) != 16)) {
		return nil, errors.New("Invalid operator authentication record")
	}
	if a.record.Sessions == nil {
		a.record.Sessions = make(map[string]time.Time)
	}
	for key := range a.record.Sessions {
		decoded, err := hex.DecodeString(key)
		if err != nil || len(decoded) != 32 {
			return nil, errors.New("Invalid operator session record")
		}
	}
	if err := os.Chmod(a.path, 0600); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *OperatorAuth) save(next operatorAuthRecord) error {
	b, err := json.Marshal(next)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(a.path), ".operator-auth-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(f.Name(), a.path); err != nil {
		return err
	}
	a.record = next
	return nil
}

// SetOperatorPassword is a local setup operation, never an Agent API operation.
func SetOperatorPassword(dir, password string) error {
	if len(password) < 8 || len(password) > 256 {
		return errors.New("Administrator password must be 8–256 bytes")
	}
	if _, err := os.Stat(filepath.Join(dir, "server.lock")); err == nil {
		return errors.New("Stop the Grantide server before setting its administrator password")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	a, err := OpenOperatorAuth(dir)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err = rand.Read(salt); err != nil {
		return err
	}
	digest, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return err
	}
	return a.save(operatorAuthRecord{Schema: 1, Salt: salt, Digest: digest, Sessions: make(map[string]time.Time)})
}

func sessionHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (a *OperatorAuth) Authorized(token string) bool {
	if a == nil || len(token) != 64 {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.record.Sessions[sessionHash(token)]
	return ok && a.now().Before(expiry)
}
func (a *OperatorAuth) Login(password, bearer, operator string, remember bool) (string, time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	recent := a.failures[:0]
	for _, t := range a.failures {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	a.failures = recent
	if len(recent) >= 5 {
		return "", time.Time{}, errOperatorThrottle
	}
	valid := false
	if bearer != "" && password == "" {
		valid = operator != "" && subtle.ConstantTimeCompare([]byte(bearer), []byte(operator)) == 1
	}
	if password != "" && bearer == "" && len(password) <= 256 && len(a.record.Digest) == 32 {
		digest, err := pbkdf2.Key(sha256.New, password, a.record.Salt, passwordIterations, 32)
		valid = err == nil && subtle.ConstantTimeCompare(digest, a.record.Digest) == 1
	}
	if !valid {
		a.failures = append(a.failures, now)
		return "", time.Time{}, errOperatorCredentials
	}
	duration := 12 * time.Hour
	if remember {
		duration = 30 * 24 * time.Hour
	}
	expiry := now.Add(duration)
	token := randomToken()
	sessions := make(map[string]time.Time)
	for k, v := range a.record.Sessions {
		if now.Before(v) {
			sessions[k] = v
		}
	}
	if len(sessions) >= 32 {
		oldest := ""
		for k, v := range sessions {
			if oldest == "" || v.Before(sessions[oldest]) {
				oldest = k
			}
		}
		delete(sessions, oldest)
	}
	sessions[sessionHash(token)] = expiry
	next := a.record
	next.Sessions = sessions
	if err := a.save(next); err != nil {
		return "", time.Time{}, errors.New("Could not persist administrator session")
	}
	a.failures = nil
	return token, expiry, nil
}
func (a *OperatorAuth) Logout(token string) error {
	if a == nil || token == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	sessions := make(map[string]time.Time)
	key := sessionHash(token)
	for k, v := range a.record.Sessions {
		if k != key {
			sessions[k] = v
		}
	}
	next := a.record
	next.Sessions = sessions
	return a.save(next)
}
