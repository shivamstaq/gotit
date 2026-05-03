package runner

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ohler55/ojg/jp"
	"github.com/ohler55/ojg/oj"
)

// ExtractValue extracts a value from stdout using a capture expression:
//   - "$.path.to.value" — JSONPath
//   - "regex:(pattern)" — regex; first capture group, or full match
func ExtractValue(stdout string, expr string) (string, error) {
	if strings.HasPrefix(expr, "regex:") {
		return extractRegex(stdout, strings.TrimPrefix(expr, "regex:"))
	}
	return extractJSONPath(stdout, expr)
}

func extractJSONPath(stdout string, pathExpr string) (string, error) {
	parsed, err := oj.ParseString(stdout)
	if err != nil {
		return "", fmt.Errorf("parse JSON for capture: %w", err)
	}
	jpath, err := jp.ParseString(pathExpr)
	if err != nil {
		return "", fmt.Errorf("parse JSONPath %q: %w", pathExpr, err)
	}
	results := jpath.Get(parsed)
	if len(results) == 0 {
		return "", fmt.Errorf("JSONPath %q matched nothing", pathExpr)
	}
	return valueToString(results[0]), nil
}

func extractRegex(stdout string, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("compile regex %q: %w", pattern, err)
	}
	matches := re.FindStringSubmatch(stdout)
	if matches == nil {
		return "", fmt.Errorf("regex %q did not match", pattern)
	}
	if len(matches) > 1 {
		return matches[1], nil
	}
	return matches[0], nil
}

// ResolveTemplates substitutes {{ var }} and {{var}} placeholders.
func ResolveTemplates(s string, vars map[string]string) string {
	for name, val := range vars {
		s = strings.ReplaceAll(s, "{{ "+name+" }}", val)
		s = strings.ReplaceAll(s, "{{"+name+"}}", val)
	}
	return s
}

func valueToString(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case int64:
		return fmt.Sprintf("%d", val)
	case bool:
		return fmt.Sprintf("%t", val)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(val)
		return string(b)
	}
}
