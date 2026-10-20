package main

import (
	"strings"
	"testing"
)

// FuzzMeasurementInputs checks external file parsing without network requests.
func FuzzMeasurementInputs(f *testing.F) {
	f.Add([]byte("package,mal_versions\nnode-ipc,12.0.1\n"), []byte(`[{"package":"node-ipc","version":"12.0.1"}]`), []byte(`{"packages":{"node_modules/node-ipc":{"version":"12.0.0"}}}`))
	f.Add([]byte("broken"), []byte("{"), []byte(`{"packages":[]}`))
	f.Fuzz(func(t *testing.T, population, cases, lockfile []byte) {
		if len(population) > 1<<20 || len(cases) > 1<<20 || len(lockfile) > 1<<20 {
			return
		}
		_, _ = parsePopulation(strings.NewReader(string(population)))
		_, _ = parseCases(cases)
		_, _ = parseLockfileNames(lockfile)
	})
}
