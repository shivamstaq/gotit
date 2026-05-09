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
// Custom types fall through to the registry passed in. The Type field on the
// returned result is left blank — callers fill it from the spec.
func RunAssertion(a Assertion, result StepResult, vars map[string]string, custom map[string]AssertionFunc) AssertionResult {
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
		return failed(a.Type, "", "",
			fmt.Errorf("assertion type %q not registered (custom types must be in Config.Assertions)", a.Type))
	}
	return failed(a.Type, "", "",
		fmt.Errorf("unknown assertion type %q (custom types must use \"x-\" prefix)", a.Type))
}

// --- built-in assertions ---

func assertExitCode(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	expected := toInt(a.Expected)
	sum := fmt.Sprintf("exit_code == %d", expected)
	want := strconv.Itoa(expected)
	got := strconv.Itoa(r.ExitCode)
	if r.ExitCode != expected {
		return failed(sum, want, got,
			fmt.Errorf("exit_code: got %d, want %d\nstderr: %s", r.ExitCode, expected, truncate(r.Stderr, 500)))
	}
	return passed(sum, want, got)
}

func assertContains(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	sum := fmt.Sprintf("stdout contains %q", a.Value)
	want := fmt.Sprintf("%q", a.Value)
	if !strings.Contains(r.Stdout, a.Value) {
		return failed(sum, want, snippetLine(r.Stdout, 80),
			fmt.Errorf("contains: output does not contain %q\noutput: %s", a.Value, truncate(r.Stdout, 500)))
	}
	return passed(sum, want, "matched")
}

func assertNotContains(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	sum := fmt.Sprintf("stdout does not contain %q", a.Value)
	want := fmt.Sprintf("%q", a.Value)
	if strings.Contains(r.Stdout, a.Value) {
		return failed(sum, want, snippetLine(r.Stdout, 80),
			fmt.Errorf("not_contains: output unexpectedly contains %q", a.Value))
	}
	return passed(sum, want, "absent")
}

func assertStderrContains(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	sum := fmt.Sprintf("stderr contains %q", a.Value)
	want := fmt.Sprintf("%q", a.Value)
	if !strings.Contains(r.Stderr, a.Value) {
		return failed(sum, want, snippetLine(r.Stderr, 80),
			fmt.Errorf("stderr_contains: stderr does not contain %q\nstderr: %s", a.Value, truncate(r.Stderr, 500)))
	}
	return passed(sum, want, "matched")
}

func assertRegex(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	sum := fmt.Sprintf("stdout matches /%s/", a.Pattern)
	want := "/" + a.Pattern + "/"
	re, err := regexp.Compile(a.Pattern)
	if err != nil {
		return failed(sum, want, "",
			fmt.Errorf("regex: invalid pattern %q: %w", a.Pattern, err))
	}
	if !re.MatchString(r.Stdout) {
		return failed(sum, want, snippetLine(r.Stdout, 80),
			fmt.Errorf("regex: output does not match /%s/\noutput: %s", a.Pattern, truncate(r.Stdout, 500)))
	}
	return passed(sum, want, "matched")
}

// assertGoldenFile resolves the path against the project root via the env var
// set by the runner, then trim-compares stdout to the file contents.
func assertGoldenFile(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	path := a.Path
	if path == "" {
		if s, ok := a.Expected.(string); ok {
			path = s
		}
	}
	displayPath := path
	if !filepath.IsAbs(path) {
		root := lookupGoldenRoot()
		if root != "" {
			path = filepath.Join(root, path)
		}
	}
	sum := fmt.Sprintf("stdout matches %s", displayPath)
	want := displayPath
	expected, err := os.ReadFile(path)
	if err != nil {
		return failed(sum, want, "",
			fmt.Errorf("golden_file: read %s: %w", path, err))
	}
	if strings.TrimSpace(r.Stdout) != strings.TrimSpace(string(expected)) {
		return failed(sum, want, firstDiverging(string(expected), r.Stdout, 80),
			fmt.Errorf("golden_file: output differs from %s\ngot (first 500 chars): %s", path, truncate(r.Stdout, 500)))
	}
	return passed(sum, want, "matched")
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

func assertJSONPath(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	op := a.Op
	if op == "" {
		op = "=="
	}
	sum := fmt.Sprintf("%s %s %v", a.Path, op, a.Expected)
	want := fmt.Sprintf("%v", a.Expected)

	parsed, err := oj.ParseString(r.Stdout)
	if err != nil {
		return failed(sum, want, "",
			fmt.Errorf("json_path: parse JSON: %w\noutput: %s", err, truncate(r.Stdout, 300)))
	}
	jpath, err := jp.ParseString(a.Path)
	if err != nil {
		return failed(sum, want, "",
			fmt.Errorf("json_path: invalid path %q: %w", a.Path, err))
	}
	results := jpath.Get(parsed)
	if len(results) == 0 {
		return failed(sum, want, "(no match)",
			fmt.Errorf("json_path: %q matched nothing", a.Path))
	}
	actual := results[0]
	got := snippetLine(valueToString(actual), 80)

	switch op {
	case "==":
		if !jsonEqual(actual, a.Expected) {
			return failed(sum, want, got,
				fmt.Errorf("json_path: %s == %v, want %v (got type %T)", a.Path, actual, a.Expected, actual))
		}
	case "!=":
		if jsonEqual(actual, a.Expected) {
			return failed(sum, want, got,
				fmt.Errorf("json_path: %s should not equal %v", a.Path, a.Expected))
		}
	case ">", ">=", "<", "<=":
		if err := compareNumeric(a.Path, op, actual, a.Expected); err != nil {
			return failed(sum, want, got, err)
		}
	case "contains":
		if err := jsonContains(a.Path, actual, a.Expected); err != nil {
			return failed(sum, want, got, err)
		}
	case "contains_any":
		if err := jsonContainsAny(a.Path, results, a.Expected); err != nil {
			return failed(sum, want, snippetLine(fmt.Sprintf("%v", results), 80), err)
		}
	case "length_gte":
		if err := jsonLengthGte(a.Path, actual, a.Expected); err != nil {
			return failed(sum, want, got, err)
		}
	default:
		return failed(sum, want, got, fmt.Errorf("json_path: unknown op %q", op))
	}
	return passed(sum, want, snippetLine(valueToString(actual), 40))
}

func assertCount(a Assertion, r StepResult, _ map[string]string) AssertionResult {
	op := a.Op
	if op == "" {
		op = "=="
	}
	expected := toInt(a.Expected)
	sum := fmt.Sprintf("count(%s) %s %d", a.Path, op, expected)
	want := strconv.Itoa(expected)

	parsed, err := oj.ParseString(r.Stdout)
	if err != nil {
		return failed(sum, want, "", fmt.Errorf("count: parse JSON: %w", err))
	}
	jpath, err := jp.ParseString(a.Path)
	if err != nil {
		return failed(sum, want, "", fmt.Errorf("count: invalid path %q: %w", a.Path, err))
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
	got := strconv.Itoa(length)

	if err := compareInts(fmt.Sprintf("count(%s)", a.Path), op, length, expected); err != nil {
		return failed(sum, want, got, err)
	}
	return passed(sum, want, got)
}

// --- result helpers ---

func passed(summary, want, got string) AssertionResult {
	return AssertionResult{Passed: true, Summary: summary, Want: want, Got: got}
}

func failed(summary, want, got string, err error) AssertionResult {
	r := AssertionResult{Passed: false, Summary: summary, Want: want, Got: got}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}

// snippetLine collapses newlines to "\n" and truncates to max runes so the
// caller can render Got on a single line in the TUI.
func snippetLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", " ")
	if len(s) > max {
		return s[:max] + "..."
	}
	return s
}

// firstDiverging returns the first line of `got` that doesn't match `want`,
// truncated to max chars. Falls back to a snippet of got if the diff is at EOF.
func firstDiverging(want, got string, max int) string {
	wlines := strings.Split(want, "\n")
	glines := strings.Split(got, "\n")
	for i := 0; i < len(glines); i++ {
		if i >= len(wlines) || glines[i] != wlines[i] {
			line := glines[i]
			if len(line) > max {
				line = line[:max] + "..."
			}
			return fmt.Sprintf("line %d: %s", i+1, line)
		}
	}
	return snippetLine(got, max)
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
