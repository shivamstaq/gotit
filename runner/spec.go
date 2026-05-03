package runner

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SupportedSpecVersion is the major version of the spec format this runner
// understands. Specs declaring a higher major are refused with a clear error.
const SupportedSpecVersion = 1

// Spec is one E2E scenario loaded from a YAML file.
type Spec struct {
	SpecVersion int            `yaml:"spec_version"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Tags        []string       `yaml:"tags"`
	Requires    []string       `yaml:"requires"`
	Repo        RepoSpec       `yaml:"repo"`
	Setup       []Step         `yaml:"setup"`
	Steps       []Step         `yaml:"steps"`
	Cleanup     []CleanupCheck `yaml:"cleanup"`

	// NotesFile optionally overrides the default sidecar notes path.
	// Default: <spec-file-without-ext>.md.
	NotesFile string `yaml:"notes_file"`

	// Order controls sort position within a wave; lower values come first.
	// Unset (0) sorts after all ordered specs, then alphabetically by name.
	Order int `yaml:"order,omitempty"`

	// Set by the loader.
	Wave      string `yaml:"-"`
	FilePath  string `yaml:"-"`
	NotesPath string `yaml:"-"`
}

// RepoSpec selects the repository the spec runs against.
type RepoSpec struct {
	Fixture    string         `yaml:"fixture"`
	Helper     string         `yaml:"helper"`
	HelperArgs map[string]any `yaml:"helper_args"`
}

// Step is one CLI invocation with optional capture and assertions.
type Step struct {
	Name    string            `yaml:"name"`
	Command string            `yaml:"command"`
	Env     map[string]string `yaml:"env"`
	Timeout string            `yaml:"timeout"`
	Capture map[string]string `yaml:"capture"`
	Assert  []Assertion       `yaml:"assert"`
}

// Assertion is one check on a step's result.
type Assertion struct {
	Type     string `yaml:"type"`
	Path     string `yaml:"path"`
	Expected any    `yaml:"expected"`
	Op       string `yaml:"op"`
	Value    string `yaml:"value"`
	Pattern  string `yaml:"pattern"`
}

// CleanupCheck is a post-test verification.
type CleanupCheck struct {
	AssertNoResidue    bool     `yaml:"assert_no_residue"`
	AssertFilesRemoved []string `yaml:"assert_files_removed"`
}

// StepResult is the outcome of executing one step.
type StepResult struct {
	Name     string
	Command  string
	ExitCode int
	Stdout   string
	Stderr   string
	Duration int64 // milliseconds
	Passed   bool
	Errors   []string
}

// LoadSpec parses one YAML spec file.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read spec %s: %w", path, err)
	}
	var spec Spec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse spec %s: %w", path, err)
	}
	if spec.SpecVersion == 0 {
		spec.SpecVersion = SupportedSpecVersion // legacy spec — apply default
	}
	if spec.SpecVersion > SupportedSpecVersion {
		return nil, fmt.Errorf("spec %s: spec_version %d is newer than this runner supports (max %d); upgrade the gotit runner",
			path, spec.SpecVersion, SupportedSpecVersion)
	}
	spec.FilePath = path
	spec.NotesPath = resolveNotesPath(path, spec.NotesFile)
	return &spec, nil
}

// LoadSpecs walks specsDir and returns every .yaml spec, sorted by
// (wave, order, name). The wave is the first path segment under specsDir.
func LoadSpecs(specsDir string) ([]*Spec, error) {
	var specs []*Spec
	err := filepath.Walk(specsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		spec, err := LoadSpec(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(specsDir, path)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 {
			spec.Wave = parts[0]
		}
		specs = append(specs, spec)
		return nil
	})
	if err != nil {
		return specs, err
	}
	sort.SliceStable(specs, func(i, j int) bool {
		if specs[i].Wave != specs[j].Wave {
			return specs[i].Wave < specs[j].Wave
		}
		oi, oj := orderKey(specs[i].Order), orderKey(specs[j].Order)
		if oi != oj {
			return oi < oj
		}
		return specs[i].Name < specs[j].Name
	})
	return specs, nil
}

func orderKey(o int) int {
	if o == 0 {
		const maxInt = int(^uint(0) >> 1)
		return maxInt
	}
	return o
}

func resolveNotesPath(specPath, notesFile string) string {
	if notesFile != "" {
		if filepath.IsAbs(notesFile) {
			return notesFile
		}
		return filepath.Join(filepath.Dir(specPath), notesFile)
	}
	ext := filepath.Ext(specPath)
	return strings.TrimSuffix(specPath, ext) + ".md"
}
