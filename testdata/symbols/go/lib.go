// Package lib is a fixture for symbol extraction.
package lib

// MaxItems caps a list. It is a constant.
const MaxItems = 10

// Grouped constants: the group doc is not given to each one.
const (
	// ModeFast runs quickly.
	ModeFast   = "fast"
	ModeSlow   = "slow" // trailing comment documents it
	modeHidden = "x"
)

// Default is the default store.
var Default = &Store{}

// Store keeps items.
type Store struct{ items []string }

type hidden struct{}

// Add appends an item to the store. More detail follows here.
func (s *Store) Add(item string) { s.items = append(s.items, item) }

// Len is on a value receiver.
func (s Store) Len() int { return len(s.items) }

func (h hidden) Exported() {}

// Set is generic.
type Set[T comparable] map[T]bool

// Has reports membership.
func (s Set[T]) Has(v T) bool { return s[v] }

// ParseImportLine parses one import line.
func ParseImportLine(line string) string { return line }

func unexported() {}
