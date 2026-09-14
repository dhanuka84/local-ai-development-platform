package execution

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

// A model can return complete bounded file contents. The trusted runtime
// constructs hunk counts deterministically; the independent evaluator still
// checks the exact resulting patch, allowed paths and protected product tests.
func FilePatch(base, replacements map[string]string) (string, error) {
	if len(replacements) < 1 || len(replacements) > 30 {
		return "", domain.ErrBudgetExhausted
	}
	names := []string{}
	for name := range replacements {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	lines := func(content string) []string {
		if content == "" {
			return nil
		}
		return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	}
	for _, name := range names {
		if path.Clean(name) != name || path.IsAbs(name) || name == "." || name == ".." || strings.HasPrefix(name, "../") || strings.ContainsAny(name, "\\\x00\r\n\t \"") {
			return "", domain.ErrForbidden
		}
		old, exists := base[name]
		next := replacements[name]
		if !utf8.ValidString(next) || strings.ContainsRune(next, 0) || len(next) > 65536 {
			return "", domain.ErrBudgetExhausted
		}
		if exists && old == next {
			continue
		}
		a, b := lines(old), lines(next)
		fmt.Fprintf(&out, "diff --git a/%s b/%s\n", name, name)
		if exists {
			fmt.Fprintf(&out, "--- a/%s\n", name)
		} else {
			out.WriteString("new file mode 100644\n--- /dev/null\n")
		}
		fmt.Fprintf(&out, "+++ b/%s\n", name)
		if len(a) == 0 && len(b) == 0 {
			continue
		}
		aStart, bStart := 1, 1
		if len(a) == 0 {
			aStart = 0
		}
		if len(b) == 0 {
			bStart = 0
		}
		fmt.Fprintf(&out, "@@ -%d,%d +%d,%d @@\n", aStart, len(a), bStart, len(b))
		for _, side := range []struct {
			prefix  string
			lines   []string
			content string
		}{{"-", a, old}, {"+", b, next}} {
			for i, line := range side.lines {
				out.WriteString(side.prefix + line + "\n")
				if i == len(side.lines)-1 && !strings.HasSuffix(side.content, "\n") {
					out.WriteString("\\ No newline at end of file\n")
				}
			}
		}
		if out.Len() > 262144 {
			return "", domain.ErrBudgetExhausted
		}
	}
	if out.Len() == 0 {
		return "", domain.ErrValidationRequired
	}
	return out.String(), nil
}
