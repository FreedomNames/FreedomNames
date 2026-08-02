// Package authoring owns Freedom Names owner keys and the construction of
// signed records. Both the human CLI and the local HTTP authoring API use this
// package so key layout and signing semantics have one implementation.
package authoring

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/routing"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

var (
	// ErrInvalidLabel means a label cannot safely or canonically identify a
	// Freedom name and its key file.
	ErrInvalidLabel = errors.New("invalid name label")
	// ErrInvalidRecords means the requested resource-record set is not valid.
	ErrInvalidRecords = errors.New("invalid resource records")
	// ErrNameExists means CreateName was asked to replace an owner key. Owner
	// keys are never overwritten: doing so would permanently lose the name.
	ErrNameExists = errors.New("name key already exists")
	// ErrNameNotFound means no owner key exists for the requested label.
	ErrNameNotFound = errors.New("name key not found")
	// ErrSequenceExhausted means the current record already uses MaxUint64 and
	// therefore no strictly newer record can be constructed.
	ErrSequenceExhausted = errors.New("record sequence exhausted")
	// ErrPublisherNotReady means the local node cannot publish records yet.
	ErrPublisherNotReady = errors.New("record publisher not ready")
	// ErrCurrentRecordUnavailable means publication could not safely choose a
	// sequence because the current network record could not be checked.
	ErrCurrentRecordUnavailable = errors.New("current record unavailable")
)

// Name is the public, non-secret description of one locally owned name.
type Name struct {
	Label string `json:"label"`
	Name  string `json:"name"`
}

// RecordPublisher is the local node behavior needed for an atomic
// resolve-sequence-sign-publish operation.
type RecordPublisher interface {
	IsInitialized() bool
	ResolveRecord(ctx context.Context, key string) (*record.FNRecord, error)
	PublishRecord(rec *record.FNRecord) error
}

// Service manages owner keys under one directory. A Service serializes
// publications per label so two local clients cannot choose the same sequence
// number concurrently.
type Service struct {
	keysDir   string
	publisher RecordPublisher

	locksMu sync.Mutex
	locks   map[string]*sync.Mutex
}

// DefaultKeysDir returns the conventional ~/.freedom/keys path.
func DefaultKeysDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".freedom", "keys"), nil
}

// New constructs an authoring service using keysDir.
func New(keysDir string, publisher RecordPublisher) (*Service, error) {
	if keysDir == "" {
		return nil, errors.New("keys directory cannot be empty")
	}
	if err := os.MkdirAll(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("create keys directory: %w", err)
	}
	if err := os.Chmod(keysDir, 0700); err != nil {
		return nil, fmt.Errorf("secure keys directory: %w", err)
	}
	return &Service{keysDir: keysDir, publisher: publisher, locks: make(map[string]*sync.Mutex)}, nil
}

// NewDefault constructs an authoring service using ~/.freedom/keys.
func NewDefault(publisher RecordPublisher) (*Service, error) {
	dir, err := DefaultKeysDir()
	if err != nil {
		return nil, err
	}
	return New(dir, publisher)
}

// CheckLabel rejects labels that cannot safely and canonically become names
// and key filenames.
func CheckLabel(label string) error {
	if label == "" {
		return fmt.Errorf("%w: label cannot be empty", ErrInvalidLabel)
	}
	if len(label) > record.MaxLabelLen {
		return fmt.Errorf("%w: label is %d bytes, max %d", ErrInvalidLabel, len(label), record.MaxLabelLen)
	}
	if label == "." || label == ".." || strings.HasPrefix(label, "-") {
		return fmt.Errorf("%w: %q", ErrInvalidLabel, label)
	}
	for _, c := range label {
		if c == '.' || c == '-' || c == '_' ||
			c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
			continue
		}
		return fmt.Errorf("%w: character %q in label %q (use a-z 0-9 . - _)", ErrInvalidLabel, c, label)
	}
	if strings.Contains(label, "..") {
		return fmt.Errorf("%w: %q", ErrInvalidLabel, label)
	}
	return nil
}

func (s *Service) keyPath(label string) (string, error) {
	if err := CheckLabel(label); err != nil {
		return "", err
	}
	return filepath.Join(s.keysDir, label+".key"), nil
}

// LoadKey loads the private owner key for label. It is exposed only to other
// internal packages; HTTP responses never include private key material.
func (s *Service) LoadKey(label string) (crypto.PrivKey, error) {
	path, err := s.keyPath(label)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w for %q", ErrNameNotFound, label)
	}
	if err != nil {
		return nil, fmt.Errorf("read key for %q: %w", label, err)
	}
	priv, err := crypto.UnmarshalPrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("decode key for %q: %w", label, err)
	}
	return priv, nil
}

func nameForKey(label string, priv crypto.PrivKey) (Name, error) {
	pub, err := crypto.MarshalPublicKey(priv.GetPublic())
	if err != nil {
		return Name{}, err
	}
	id, err := record.PubKeyID(pub)
	if err != nil {
		return Name{}, err
	}
	return Name{Label: label, Name: label + "." + id + "." + record.TLD}, nil
}

// Name returns the public name derived from label's owner key.
func (s *Service) Name(label string) (Name, error) {
	priv, err := s.LoadKey(label)
	if err != nil {
		return Name{}, err
	}
	return nameForKey(label, priv)
}

// ListNames returns locally owned names sorted by label. A corrupt key is kept
// in the result with an empty Name so one damaged file does not hide the other
// usable names or silently free its label for replacement.
func (s *Service) ListNames() ([]Name, error) {
	entries, err := os.ReadDir(s.keysDir)
	if err != nil {
		return nil, fmt.Errorf("read keys directory: %w", err)
	}
	names := make([]Name, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".key") {
			continue
		}
		label := strings.TrimSuffix(entry.Name(), ".key")
		if label == "" {
			continue
		}
		name, err := s.Name(label)
		if err != nil {
			names = append(names, Name{Label: label})
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i].Label < names[j].Label })
	return names, nil
}

// CreateName creates a new Ed25519 owner key without ever overwriting an
// existing key. The private key file is owner-readable only.
func (s *Service) CreateName(label string) (Name, error) {
	path, err := s.keyPath(label)
	if err != nil {
		return Name{}, err
	}
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.Ed25519, -1, rand.Reader)
	if err != nil {
		return Name{}, err
	}
	data, err := crypto.MarshalPrivateKey(priv)
	if err != nil {
		return Name{}, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		return Name{}, fmt.Errorf("%w for %q", ErrNameExists, label)
	}
	if err != nil {
		return Name{}, fmt.Errorf("create key for %q: %w", label, err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		_ = os.Remove(path)
		return Name{}, fmt.Errorf("write key for %q: %w", label, err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return Name{}, fmt.Errorf("close key for %q: %w", label, err)
	}
	return nameForKey(label, priv)
}

// BuildRecord validates records and signs them with a sequence strictly above
// current. It is used by the CLI after its HTTP /record lookup.
func (s *Service) BuildRecord(label string, records []record.RR, current *record.FNRecord) (*record.FNRecord, error) {
	if err := (&record.FNRecord{Label: label, Records: records}).ValidateRecords(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRecords, err)
	}
	priv, err := s.LoadKey(label)
	if err != nil {
		return nil, err
	}
	seq, err := nextSeq(uint64(time.Now().Unix()), current)
	if err != nil {
		return nil, err
	}
	return record.BuildAndSignRecord(priv, label, records, seq)
}

// Publish performs a complete local publication while holding the label lock:
// resolve the current sequence, build and sign the new record, then publish it.
func (s *Service) Publish(ctx context.Context, label string, records []record.RR) (*record.FNRecord, error) {
	if s.publisher == nil {
		return nil, errors.New("authoring service has no record publisher")
	}
	if !s.publisher.IsInitialized() {
		return nil, ErrPublisherNotReady
	}
	if err := CheckLabel(label); err != nil {
		return nil, err
	}
	name, err := s.Name(label)
	if err != nil {
		return nil, err
	}
	lock := s.labelLock(label)
	lock.Lock()
	defer lock.Unlock()

	key, err := record.DHTKeyForName(name.Name)
	if err != nil {
		return nil, err
	}
	current, err := s.publisher.ResolveRecord(ctx, key)
	if err != nil && !errors.Is(err, routing.ErrNotFound) {
		return nil, fmt.Errorf("%w: %v", ErrCurrentRecordUnavailable, err)
	}
	rec, err := s.BuildRecord(label, records, current)
	if err != nil {
		return nil, err
	}
	if err := s.publisher.PublishRecord(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *Service) labelLock(label string) *sync.Mutex {
	s.locksMu.Lock()
	defer s.locksMu.Unlock()
	lock := s.locks[label]
	if lock == nil {
		lock = &sync.Mutex{}
		s.locks[label] = lock
	}
	return lock
}

func nextSeq(wallClock uint64, current *record.FNRecord) (uint64, error) {
	if current != nil && current.Seq >= wallClock {
		if current.Seq == math.MaxUint64 {
			return 0, ErrSequenceExhausted
		}
		return current.Seq + 1, nil
	}
	return wallClock, nil
}
