package reporting

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"sync"
	"time"
)

//go:embed templates/*.tmpl templates/partials/*.tmpl templates/reports/*.tmpl
var templateFS embed.FS

// funcMap holds the helpers available to every report template. Methods on the
// data types (Severity.Color, RiskRow.Score, etc.) cover most needs; this is
// only for things templates cannot express directly.
var funcMap = template.FuncMap{
	"inc":        func(n int) int { return n + 1 },
	"formatDate": func(t time.Time) string { return t.Format("02 January 2006") },
	"formatTime": func(t time.Time) string { return t.Format("02 January 2006 15:04 MST") },
	"year":       func(t time.Time) int { return t.Year() },
}

// commonPatterns are the layout + partials parsed into every report template.
var commonPatterns = []string{
	"templates/base.tmpl",
	"templates/partials/*.tmpl",
}

var (
	tmplCacheMu sync.RWMutex
	tmplCache   = map[string]*template.Template{}
)

// reportTemplate parses (and caches) the base layout, all partials, and the
// named report content template into a single set. The returned template is
// executed by its "base" definition.
func reportTemplate(reportFile string) (*template.Template, error) {
	tmplCacheMu.RLock()
	cached, ok := tmplCache[reportFile]
	tmplCacheMu.RUnlock()
	if ok {
		return cached, nil
	}

	patterns := append([]string{}, commonPatterns...)
	patterns = append(patterns, "templates/reports/"+reportFile)

	tmpl, err := template.New("base").Funcs(funcMap).ParseFS(templateFS, patterns...)
	if err != nil {
		return nil, fmt.Errorf("parse report template %q: %w", reportFile, err)
	}

	tmplCacheMu.Lock()
	tmplCache[reportFile] = tmpl
	tmplCacheMu.Unlock()
	return tmpl, nil
}

// renderHTML executes the given report template against doc and returns the
// full HTML document bytes.
func renderHTML(reportFile string, doc Document) ([]byte, error) {
	tmpl, err := reportTemplate(reportFile)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "base", doc); err != nil {
		return nil, fmt.Errorf("execute report template %q: %w", reportFile, err)
	}
	return buf.Bytes(), nil
}

// renderHeaderFooter executes the shared "pdf_header" / "pdf_footer" partials
// (parsed into every report set) for the chromedp running header/footer.
func renderHeaderFooter(reportFile string, doc Document) (header, footer string, err error) {
	tmpl, err := reportTemplate(reportFile)
	if err != nil {
		return "", "", err
	}
	var hb, fb bytes.Buffer
	if err := tmpl.ExecuteTemplate(&hb, "pdf_header", doc); err != nil {
		return "", "", fmt.Errorf("execute pdf_header: %w", err)
	}
	if err := tmpl.ExecuteTemplate(&fb, "pdf_footer", doc); err != nil {
		return "", "", fmt.Errorf("execute pdf_footer: %w", err)
	}
	return hb.String(), fb.String(), nil
}
