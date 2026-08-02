package authoring

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/libp2p/go-libp2p/core/routing"
	"gitlab.melroy.org/freedom-names/freedom-names/internal/record"
)

func TestCheckLabel(t *testing.T) {
	bad := []string{"", "..", ".", "../../etc/passwd", "a/b", `a\b`, "a..b", "-lead", "sp ace", "nul\x00l"}
	for _, label := range bad {
		if err := CheckLabel(label); !errors.Is(err, ErrInvalidLabel) {
			t.Errorf("CheckLabel(%q) = %v, want ErrInvalidLabel", label, err)
		}
	}
	good := []string{"mysite", "blog.mysite", "my-site", "my_site", "site123"}
	for _, label := range good {
		if err := CheckLabel(label); err != nil {
			t.Errorf("CheckLabel(%q) = %v", label, err)
		}
	}
}

func TestCreateNameListAndPermissions(t *testing.T) {
	dir := t.TempDir()
	service, err := New(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	blog, err := service.CreateName("blog")
	if err != nil {
		t.Fatalf("create blog: %v", err)
	}
	if blog.Label != "blog" || blog.Name == "" {
		t.Fatalf("unexpected created name: %+v", blog)
	}
	if _, err := service.CreateName("alpha"); err != nil {
		t.Fatalf("create alpha: %v", err)
	}
	if _, err := service.CreateName("blog"); !errors.Is(err, ErrNameExists) {
		t.Fatalf("duplicate create = %v, want ErrNameExists", err)
	}

	info, err := os.Stat(filepath.Join(dir, "blog.key"))
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("key mode = %o, want 600", got)
	}
	if got := mustMode(t, dir); got != 0700 {
		t.Fatalf("keys dir mode = %o, want 700", got)
	}

	// Staging files are not names. A corrupt key remains visible by label but
	// cannot leak bytes or stop valid names from being listed.
	if err := os.WriteFile(filepath.Join(dir, "alpha.records.json"), []byte("[]"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.key"), []byte("not a key"), 0600); err != nil {
		t.Fatal(err)
	}
	names, err := service.ListNames()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 3 || names[0].Label != "alpha" || names[1] != blog || names[2] != (Name{Label: "broken"}) {
		t.Fatalf("unexpected names: %+v", names)
	}
}

func TestCreateNameConcurrentDoesNotOverwrite(t *testing.T) {
	service, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := service.CreateName("same")
			errs <- err
		}()
	}
	var successes, conflicts int
	for range 2 {
		err := <-errs
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrNameExists):
			conflicts++
		default:
			t.Fatalf("unexpected create error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d, want 1/1", successes, conflicts)
	}
}

func TestBuildRecordValidatesAndAdvancesSequence(t *testing.T) {
	service, err := New(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateName("blog"); err != nil {
		t.Fatal(err)
	}
	records := []record.RR{{Type: record.RecordTypeA, Value: "10.0.0.5", TTL: 300}}
	current := &record.FNRecord{Seq: math.MaxInt64}
	rec, err := service.BuildRecord("blog", records, current)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Seq != uint64(math.MaxInt64)+1 {
		t.Fatalf("seq = %d, want %d", rec.Seq, uint64(math.MaxInt64)+1)
	}
	if err := rec.Verify(); err != nil {
		t.Fatalf("signed record does not verify: %v", err)
	}
	if _, err := service.BuildRecord("blog", nil, nil); !errors.Is(err, ErrInvalidRecords) {
		t.Fatalf("empty records = %v, want ErrInvalidRecords", err)
	}
	if _, err := service.BuildRecord("blog", records, &record.FNRecord{Seq: math.MaxUint64}); !errors.Is(err, ErrSequenceExhausted) {
		t.Fatalf("max sequence = %v, want ErrSequenceExhausted", err)
	}
}

type memoryPublisher struct {
	mu        sync.Mutex
	current   *record.FNRecord
	sequences []uint64
}

func (p *memoryPublisher) IsInitialized() bool { return true }

func (p *memoryPublisher) ResolveRecord(context.Context, string) (*record.FNRecord, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.current == nil {
		return nil, routing.ErrNotFound
	}
	copy := *p.current
	return &copy, nil
}

func (p *memoryPublisher) PublishRecord(rec *record.FNRecord) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	copy := *rec
	p.current = &copy
	p.sequences = append(p.sequences, rec.Seq)
	return nil
}

func TestPublishSerializesSequencePerLabel(t *testing.T) {
	publisher := &memoryPublisher{}
	service, err := New(t.TempDir(), publisher)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateName("blog"); err != nil {
		t.Fatal(err)
	}
	records := []record.RR{{Type: record.RecordTypeA, Value: "10.0.0.5", TTL: 300}}

	const publishes = 16
	var wg sync.WaitGroup
	errs := make(chan error, publishes)
	for range publishes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.Publish(context.Background(), "blog", records)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("publish: %v", err)
		}
	}

	publisher.mu.Lock()
	sequences := append([]uint64(nil), publisher.sequences...)
	publisher.mu.Unlock()
	if len(sequences) != publishes {
		t.Fatalf("got %d publications, want %d", len(sequences), publishes)
	}
	sort.Slice(sequences, func(i, j int) bool { return sequences[i] < sequences[j] })
	for i := 1; i < len(sequences); i++ {
		if sequences[i] != sequences[i-1]+1 {
			t.Fatalf("sequences are not unique and contiguous: %v", sequences)
		}
	}
}

func mustMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
