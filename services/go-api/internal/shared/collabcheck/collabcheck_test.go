package collabcheck_test

import (
	"altune/go-api/internal/shared/collabcheck"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

const fixture = `package collab

type Store interface{ Get() string }

type Hook func()

type Empty interface{}

type Service struct {
	store    Store
	hook     Hook
	any      Empty
	err      error
	optional Store
	count    *int
}

func NewService(store Store, hook Hook) *Service {
	return &Service{store: store, hook: hook}
}

func (s *Service) Run() {
	if s.store == nil { // want ` + "`nil check on collaborator field store`" + `
		return
	}
	if nil != s.hook { // want ` + "`nil check on collaborator field hook`" + `
		s.hook()
	}
	if s.any == nil || s.err != nil || s.count == nil {
		return
	}
	if s.optional == nil {
		return
	}
}

func (s *Service) Attach(store Store) { s.optional = store }

func WithStore(store Store) func(*Service) { // want ` + "`option WithStore takes a collaborator`" + `
	return func(s *Service) { s.optional = store }
}

func (s *Service) WithHook(h Hook) *Service { // want ` + "`option WithHook takes a collaborator`" + `
	s.hook = h
	return s
}

func WithLimit(n int) func(*Service) { return func(*Service) {} }

func WithValue(v Empty) func(*Service) { return func(*Service) {} }

func Without(store Store) {}

func WithRetry(store Store, do func()) (string, error) { return "", nil }

type Bag struct{ Store Store }

func NewBag(store Store) Bag { return Bag{Store: store} }

func (b Bag) Valid() bool { return b.Store != nil }
`

func TestAnalyzer_FlagsOnlyConstructorSetCollaboratorsAndCollaboratorOptions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "collab")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "collab.go"), []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}

	analysistest.Run(t, filepath.Dir(filepath.Dir(dir)), collabcheck.Analyzer, "collab")
}
