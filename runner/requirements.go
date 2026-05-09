package runner

import (
	"fmt"
	"strings"
)

// CheckBuiltinRequirements is a TUI-friendly variant of CheckRequirements
// that evaluates only the requirement types resolvable without a consumer
// runner.Config: "feature:<name>" and "git". Unknown requirements (custom
// names registered in Config.RequirementCheckers) are treated as satisfied —
// the testdriver re-runs the full check at test time, so anything the TUI
// can't pre-verify still gets the correct outcome via the JSONL skip event.
func CheckBuiltinRequirements(reqs []string, projectRoot, featuresFile, envPrefix string) error {
	for _, req := range reqs {
		if strings.HasPrefix(req, "feature:") {
			name := strings.TrimPrefix(req, "feature:")
			if !isFeatureEnabled(name, projectRoot, featuresFile, envPrefix) {
				return fmt.Errorf("feature %q not yet shipped (add to %s or set %s_E2E_FEATURES to enable)", name, featuresFile, strings.ToUpper(envPrefix))
			}
		}
	}
	return nil
}

// CheckRequirements verifies every entry in reqs against the resolution rules:
//
//   - "feature:<name>" — looked up in featuresFile (and the
//     <PREFIX>_E2E_FEATURES env override). Unknown features default to false.
//   - "git" — built-in; always satisfied (the harness assumes git is available).
//   - any other name — looked up in customCheckers. Missing names produce an
//     "unknown requirement" error.
//
// Returns the first unmet requirement, or nil if all pass.
func CheckRequirements(reqs []string, projectRoot string, customCheckers map[string]RequirementChecker, featuresFile, envPrefix string) error {
	for _, req := range reqs {
		if strings.HasPrefix(req, "feature:") {
			name := strings.TrimPrefix(req, "feature:")
			if !isFeatureEnabled(name, projectRoot, featuresFile, envPrefix) {
				return fmt.Errorf("feature %q not yet shipped (add to %s or set %s_E2E_FEATURES to enable)", name, featuresFile, strings.ToUpper(envPrefix))
			}
			continue
		}
		if req == "git" {
			continue
		}
		checker, ok := customCheckers[req]
		if !ok {
			return fmt.Errorf("unknown requirement %q (register it in Config.RequirementCheckers)", req)
		}
		if err := checker(projectRoot); err != nil {
			return fmt.Errorf("requirement %q not met: %w", req, err)
		}
	}
	return nil
}
