// Package presets writes llama-server's router-mode presets.ini: a generic
// [ModelName] section per model whose keys are llama-server long-flag names
// (without leading dashes) passed through verbatim as `key = value` lines.
// llama-server itself normalizes flag aliases (e.g. "gpu-layers" becomes
// "n-gpu-layers" in its own preset dump), so this package does not need to
// know the flag schema at all.
package presets

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Write renders overrides (model name -> flag -> value) to path.
//
// If overrides is empty, no presets.ini is needed at all (plain --models-dir
// scanning is enough), so any existing file at path is removed and wrote
// reports false — callers should omit --models-preset in that case.
func Write(path string, overrides map[string]map[string]string) (wrote bool, err error) {
	if len(overrides) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("removing stale %s: %w", path, err)
		}
		return false, nil
	}

	names := make([]string, 0, len(overrides))
	for name := range overrides {
		names = append(names, name)
	}
	sort.Strings(names)

	var b strings.Builder
	for _, name := range names {
		fmt.Fprintf(&b, "[%s]\n", name)

		keys := make([]string, 0, len(overrides[name]))
		for k := range overrides[name] {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			fmt.Fprintf(&b, "%s = %s\n", k, overrides[name][k])
		}
		b.WriteString("\n")
	}

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return false, fmt.Errorf("writing %s: %w", path, err)
	}
	return true, nil
}
