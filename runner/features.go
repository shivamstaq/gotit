package runner

import (
	"os"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// featureFlags is the on-disk shape of the allowlist.
type featureFlags struct {
	Enabled map[string]bool `yaml:"enabled"`
}

type featuresKey struct {
	path   string
	prefix string
}

var (
	featuresMu    sync.Mutex
	featuresCache = map[featuresKey]map[string]bool{}
)

// loadFeatures reads featuresFile and overlays the <prefix>_E2E_FEATURES env
// override. Result is cached per (path, prefix) tuple so repeated calls are cheap.
//
// Special override token "all" enables every flag listed in the file.
func loadFeatures(_, featuresFile, envPrefix string) map[string]bool {
	key := featuresKey{path: featuresFile, prefix: strings.ToUpper(envPrefix)}

	featuresMu.Lock()
	defer featuresMu.Unlock()
	if cached, ok := featuresCache[key]; ok {
		return cached
	}

	cache := map[string]bool{}

	if data, err := os.ReadFile(featuresFile); err == nil {
		var ff featureFlags
		if err := yaml.Unmarshal(data, &ff); err == nil {
			for k, v := range ff.Enabled {
				cache[k] = v
			}
		}
	}

	envVar := key.prefix + "_E2E_FEATURES"
	if env := os.Getenv(envVar); env != "" {
		for _, f := range strings.Split(env, ",") {
			f = strings.TrimSpace(f)
			if f == "" {
				continue
			}
			if f == "all" {
				for k := range cache {
					cache[k] = true
				}
				continue
			}
			cache[f] = true
		}
	}

	featuresCache[key] = cache
	return cache
}

// isFeatureEnabled reports whether a feature flag is shipped.
// Unknown features default to false (skip in CI).
func isFeatureEnabled(name, projectRoot, featuresFile, envPrefix string) bool {
	return loadFeatures(projectRoot, featuresFile, envPrefix)[name]
}

// ResetFeatureCache clears the cached features map. Tests that mutate
// features.yaml between runs should call this.
func ResetFeatureCache() {
	featuresMu.Lock()
	defer featuresMu.Unlock()
	featuresCache = map[featuresKey]map[string]bool{}
}
