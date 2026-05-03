package runner

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ohler55/ojg/jp"
	"github.com/ohler55/ojg/oj"
)

// builtinAssertions returns the 8 assertion types every runner ships with.
// Custom types registered in Config.Assertions extend this set; custom names
// must use the "x-" prefix and are validated when specs load.
func builtinAssertions() map[string]AssertionFunc {
	return map[string]AssertionFunc{
		"exit_code":       assertExitCode,
		"contains":        assertContains,
		"not_contains":    assertNotContains,
		"stderr_contains": assertStderrContains,
		"regex":           assertRegex,
		"json_path":       assertJSONPath,
		"count":           assertCount,
		"golden_file":     assertGoldenFile,
	}
}

// RunAssertion evaluates one assertion against a step result. It resolves
// template variables in assertion fields first, then dispatches by type.
// Custom types fall through to the registry passed in.
func RunAssertion(a Assertion, result StepResult, vars map[string]string, custom map[string]AssertionFunc) error {
	a.Value = ResolveTemplates(a.Value, vars)
	a.Pattern = ResolveTemplates(a.Pattern, vars)
	a.Path = ResolveTemplates(a.Path, vars)
	if s, ok := a.Expected.(string); ok {
		a.Expected = ResolveTemplates(s, vars)
	}

	if fn, ok := builtinAssertions()[a.Type]; ok {
		return fn(a, result, vars)
	}
	if strings.HasPrefix(a.Type, "x-") {
		if fn, ok := custom[a.Type]; ok {
			return fn(a, result, vars)
		}
		return fmt.Errorf("assertion type %q not registered (custom types must be in Config.Assertions)", a.Type)
	}
	return fmt.Errorf("unknown assertion type %q (custom types must use \"x-\" prefix)", a.Type)
}

// --- built-in assertions ---

func assertExitCode(a Assertion, r StepResult, _ map[string]string) error {
	expected := toInt(a.Expected)
	if r.ExitCode != expected {
		return fmt.Errorf("exit_code: got %d, want %d\nstderr: %s", r.ExitCode, expected, truncate(r.Stderr, 500))
	}
	return nil
}

func assertContains(a Assertion, r StepResult, _ map[string]string) error {
	if !strings.Contains(r.Stdout, a.Value) {
		return fmt.Errorf("contains: output does not contain %q\noutput: %s", a.Value, truncate(r.Stdout, 500))
	}
	return nil
}

func assertNotContains(a Assertion, r StepResult, _ map[string]string) error {
	if strings.Contains(r.Stdout, a.Value) {
		return fmt.Errorf("not_contains: output unexpectedly contains %q", a.Value)
	}
	return nil
}

func assertStderrContains(a Assertion, r StepResult, _ map[string]string) error {
	if !strings.Contains(r.Stderr, a.Value) {
		return fmt.Errorf("stderr_contains: stderr does not contain %q\nstderr: %s", a.Value, truncate(r.Stderr, 500))
	}
	return nil
}

func assertRegex(a Assertion, r StepResult, _ map[string]string) error {
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return fmt.Errorf("regex: invalid pattern %q: %w", a.Pattern, err)
	}
	if !re.MatchString(r.Stdout) {
		return fmt.Errorf("regex: output does not match /%s/\noutput: %s", a.Pattern, truncate(r.Stdout, 500))
	}
	return nil
}

// assertGoldenFile resolves the path against the project root via the env var
// set by the runner, then trim-compares stdout to the file contents.
func assertGoldenFile(a Assertion, r StepResult, _ map[string]string) error {
	path := a.Path
	if path == "" {
		if s, ok := a.Expected.(string); ok {
			path = s
		}
	}
	if !filepath.IsAbs(path) {
		root := lookupGoldenRoot()
		if root != "" {
			path = filepath.Join(root, path)
		}
	}
	expected, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("golden_file: read %s: %w", path, err)
	}
	if strings.TrimSpace(r.Stdout) != strings.TrimSpace(string(expected)) {
		return fmt.Errorf("golden_file: output differs from %s\ngot (first 500 chars): %s", path, truncate(r.Stdout, 500))
	}
	return nil
}

// lookupGoldenRoot searches the environment for any *_E2E_PROJECT_ROOT var.
// The runner sets <PREFIX>_E2E_PROJECT_ROOT under the user's EnvPrefix.
func lookupGoldenRoot() string {
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.HasSuffix(k, "_E2E_PROJECT_ROOT") {
			return v
		}
	}
	return ""
}

func assertJSONPath(a Assertion, r StepResult, _ map[string]string) error {
	parsed, err := oj.ParseString(r.Stdout)
	if err != nil {
		return fmt.Errorf("json_path: parse JSON: %w\noutput: %s", err, truncate(r.Stdout, 300))
	}
	jpath, err := jp.ParseString(a.Path)
	if err != nil {
		return fmt.Errorf("json_path: invalid path %q: %w", a.Path, err)
	}
	results := jpath.Get(parsed)
	if len(results) == 0 {
		return fmt.Errorf("json_path: %q matched nothing", a.Path)
	}

	op := a.Op
	if op == "" {
		op = "=="
	}
	actual := results[0]

	switch op {
	case "==":
		if !jsonEqual(actual, a.Expected) {
			return fmt.Errorf("json_path: %s == %v, want %v (got type %T)", a.Path, actual, a.Expected, actual)
		}
	case "!=":
		if jsonEqual(actual, a.Expected) {
			return fmt.Errorf("json_path: %s should not equal %v", a.Path, a.Expected)
		}
	case ">", ">=", "<", "<=":
		return compareNumeric(a.Path, op, actual, a.Expected)
	case "contains":
		return jsonContains(a.Path, actual, a.Expected)
	case "contains_any":
		return jsonContainsAny(a.Path, results, a.Expected)
	case "length_gte":
		return jsonLengthGte(a.Path, actual, a.Expected)
	default:
		return fmt.Errorf("json_path: unknown op %q", op)
	}
	return nil
}

func assertCount(a Assertion, r StepResult, _ map[string]string) error {
	parsed, err := oj.ParseString(r.Stdout)
	if err != nil {
		return fmt.Errorf("count: parse JSON: %w", err)
	}
	jpath, err := jp.ParseString(a.Path)
	if err != nil {
		return fmt.Errorf("count: invalid path %q: %w", a.Path, err)
	}
	results := jpath.Get(parsed)

	var length int
	if len(results) == 1 {
		if arr, ok := results[0].([]any); ok {
			length = len(arr)
		} else {
			length = len(results)
		}
	} else {
		length = len(results)
	}

	expected := toInt(a.Expected)
	op := a.Op
	if op == "" {
		op = "=="
	}
	return compareInts(fmt.Sprintf("count(%s)", a.Path), op, length, expected)
}

// --- helpers ---

func jsonEqual(actual any, expected any) bool {
	as := valueToString(actual)
	es := fmt.Sprintf("%v", expected)
	return as == es
}

func compareNumeric(path, op string, actual, expected any) error {
	a := toFloat(actual)
	e := toFloat(expected)
	var ok bool
	switch op {
	case ">":
		ok = a > e
	case ">=":
		ok = a >= e
	case "<":
		ok = a < e
	case "<=":
		ok = a <= e
	}
	if !ok {
		return fmt.Errorf("json_path: %s: %g %s %g is false", path, a, op, e)
	}
	return nil
}

func compareInts(label, op string, actual, expected int) error {
	var ok bool
	switch op {
	case "==":
		ok = actual == expected
	case "!=":
		ok = actual != expected
	case ">":
		ok = actual > expected
	case ">=":
		ok = actual >= expected
	case "<":
		ok = actual < expected
	case "<=":
		ok = actual <= expected
	}
	if !ok {
		return fmt.Errorf("%s: %d %s %d is false", label, actual, op, expected)
	}
	return nil
}

func jsonContains(path string, actual, expected any) error {
	s := valueToString(actual)
	e := fmt.Sprintf("%v", expected)
	if !strings.Contains(s, e) {
		return fmt.Errorf("json_path: %s value %q does not contain %q", path, s, e)
	}
	return nil
}

func jsonContainsAny(path string, results []any, expected any) error {
	var actuals []string
	for _, r := range results {
		switch v := r.(type) {
		case []any:
			for _, item := range v {
				actuals = append(actuals, valueToString(item))
			}
		default:
			actuals = append(actuals, valueToString(v))
		}
	}

	var expectedList []string
	switch v := expected.(type) {
	case []any:
		for _, item := range v {
			expectedList = append(expectedList, fmt.Sprintf("%v", item))
		}
	case []string:
		expectedList = v
	default:
		expectedList = []string{fmt.Sprintf("%v", expected)}
	}

	for _, e := range expectedList {
		found := false
		for _, a := range actuals {
			if a == e {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("json_path: %s does not contain %q (actuals: %v)", path, e, actuals)
		}
	}
	return nil
}

func jsonLengthGte(path string, actual, expected any) error {
	var length int
	switch v := actual.(type) {
	case []any:
		length = len(v)
	case string:
		length = len(v)
	default:
		b, _ := json.Marshal(actual)
		var arr []any
		if json.Unmarshal(b, &arr) == nil {
			length = len(arr)
		}
	}
	e := toInt(expected)
	if length < e {
		return fmt.Errorf("json_path: %s length %d < %d", path, length, e)
	}
	return nil
}

func toInt(v any) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	case string:
		n, _ := strconv.Atoi(val)
		return n
	default:
		return 0
	}
}

func toFloat(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	default:
		return math.NaN()
	}
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}
