// Package report writes report.json and report.md atomically (tmp + rename).
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/r4d1um/pi-accept/internal/model"
)

func Write(dir string, r model.Report) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	j, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := atomic(filepath.Join(dir, "report.json"), j); err != nil {
		return err
	}
	return atomic(filepath.Join(dir, "report.md"), []byte(Markdown(r)))
}

func Markdown(r model.Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pi acceptance: %s — **%s**\n\n", r.Host, r.Verdict)
	fmt.Fprintf(&b, "- Model: %s\n- Tool: piaccept v%s\n- Run: %s → %s (UTC)\n\n", r.Model, r.Version,
		r.Started.Format("2006-01-02 15:04:05"), r.Finished.Format("15:04:05"))
	group := ""
	for _, x := range r.Results {
		if x.Group != group {
			group = x.Group
			fmt.Fprintf(&b, "\n## %s\n\n| | Check | Value |\n|---|---|---|\n", group)
		}
		fmt.Fprintf(&b, "| %s | %s | %s |\n", x.Status, x.Title, strings.ReplaceAll(x.Value, "|", "\\|"))
	}
	b.WriteString("\n## Pitfalls\n\n")
	n := 0
	for _, x := range r.Results {
		if (x.Status == model.Warn || x.Status == model.Fail) && x.Hint != "" {
			n++
			fmt.Fprintf(&b, "%d. **%s %s** — %s\n", n, x.Status, x.Title, x.Hint)
		}
	}
	if n == 0 {
		b.WriteString("None. 🎉\n")
	}
	return b.String()
}

func atomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
