// Package sources wires up every Fulgora dataset source. The registry is a
// map of source name to a factory function that returns a Source.
//
// # Adding a New Source
//
//  1. Implement the source.Source interface in its own package under
//     internal/source/<name> (see internal/source/source.go for the full
//     guide and code skeleton).
//
//  2. Register the source by adding one entry to the registry map below:
//
//     "mysource": func() source.Source { return mysource.New() },
//
//     Then add the import path. The factory function is called once per
//     collection run so any per-call state should be created inside Process.
//
// Sources are independent: each one runs in its own goroutine (see the
// collect package) and must not share mutable state with other sources.
package sources

import (
	"fmt"
	"sort"

	"github.com/nexus/fulgora/internal/source"
	"github.com/nexus/fulgora/internal/source/openfunderregistry"
	"github.com/nexus/fulgora/internal/source/retractionwatch"
	"github.com/nexus/fulgora/internal/source/ror"
)

var registry = map[string]func() source.Source{
	"openfunderregistry": func() source.Source { return openfunderregistry.New() },
	"ror":                func() source.Source { return ror.New() },
	"retractionwatch":    func() source.Source { return retractionwatch.New() },
}

// All returns the names of every registered source, sorted alphabetically.
func All() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Get returns the source registered under name, or an error if none exists.
// The returned Source is freshly constructed and safe to use concurrently
// with sources returned by other calls to Get.
func Get(name string) (source.Source, error) {
	factory, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("source %q is not registered; available: %v", name, All())
	}
	return factory(), nil
}
