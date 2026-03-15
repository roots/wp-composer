package http

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"time"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

var funcMap = template.FuncMap{
	"formatNumber":      formatNumber,
	"formatNumberComma": formatNumberComma,
	"sub":               func(a, b int) int { return a - b },
	"add":               func(a, b int) int { return a + b },
	"paginate":          paginateURL,
	"paginatePartial":   paginatePartialURL,
	"adminPaginate":     adminPaginateURL,
	"jsonLD":            jsonLD,
	"formatCST":         formatCST,
	"timeAgo":           timeAgo,
}

type templateSet struct {
	index          *template.Template
	indexPartial   *template.Template
	detail         *template.Template
	compare        *template.Template
	rootsWordpress *template.Template
	notFound       *template.Template
	adminDashboard *template.Template
	adminPackages  *template.Template
	adminBuilds    *template.Template
	adminLogs      *template.Template
}

func loadTemplates(env string) *templateSet {
	funcMap["isProduction"] = func() bool { return env == "production" }
	return &templateSet{
		index:          parse("templates/layout.html", "templates/index.html", "templates/package_results.html"),
		indexPartial:   parse("templates/package_results.html"),
		detail:         parse("templates/layout.html", "templates/detail.html"),
		compare:        parse("templates/layout.html", "templates/compare.html"),
		rootsWordpress: parse("templates/layout.html", "templates/roots_wordpress.html"),
		notFound:       parse("templates/layout.html", "templates/404.html"),
		adminDashboard: parse("templates/admin_layout.html", "templates/admin_dashboard.html"),
		adminPackages:  parse("templates/admin_layout.html", "templates/admin_packages.html"),
		adminBuilds:    parse("templates/admin_layout.html", "templates/admin_builds.html"),
		adminLogs:      parse("templates/admin_layout.html", "templates/admin_logs.html"),
	}
}

func parse(files ...string) *template.Template {
	return template.Must(template.New("").Funcs(funcMap).ParseFS(templateFS, files...))
}

func render(w http.ResponseWriter, r *http.Request, tmpl *template.Template, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		captureError(r, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func formatNumber(n int64) string {
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatNumberComma(n int) string {
	s := fmt.Sprintf("%d", n)
	if n < 1000 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

type publicFilters struct {
	Search string
	Type   string
	Sort   string
}

type adminFilters struct {
	Search string
	Type   string
	Active string
}

func paginateURL(f publicFilters, page int) string {
	v := url.Values{}
	if f.Search != "" {
		v.Set("search", f.Search)
	}
	if f.Type != "" {
		v.Set("type", f.Type)
	}
	if f.Sort != "" && f.Sort != "composer_installs" {
		v.Set("sort", f.Sort)
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	q := v.Encode()
	if q == "" {
		return "/"
	}
	return "/?" + q
}

func paginatePartialURL(f publicFilters, page int) string {
	v := url.Values{}
	if f.Search != "" {
		v.Set("search", f.Search)
	}
	if f.Type != "" {
		v.Set("type", f.Type)
	}
	if f.Sort != "" && f.Sort != "composer_installs" {
		v.Set("sort", f.Sort)
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	q := v.Encode()
	if q == "" {
		return "/packages-partial"
	}
	return "/packages-partial?" + q
}

func adminPaginateURL(f adminFilters, page int) string {
	v := url.Values{}
	if f.Search != "" {
		v.Set("search", f.Search)
	}
	if f.Type != "" {
		v.Set("type", f.Type)
	}
	if f.Active != "" {
		v.Set("active", f.Active)
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	q := v.Encode()
	if q == "" {
		return "/admin/packages"
	}
	return "/admin/packages?" + q
}

func jsonLD(data any) template.HTML {
	if data == nil {
		return ""
	}
	// If it's a slice, emit one script tag per item
	if items, ok := data.([]any); ok {
		var out string
		for _, item := range items {
			b, err := json.Marshal(item)
			if err != nil {
				continue
			}
			out += `<script type="application/ld+json">` + string(b) + `</script>`
		}
		return template.HTML(out)
	}
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return template.HTML(`<script type="application/ld+json">` + string(b) + `</script>`)
}

var cst = func() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.FixedZone("CST", -6*60*60)
	}
	return loc
}()

// formatCST converts an RFC3339 string to "Jan 2, 3:04 PM" in America/Chicago.
func formatCST(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return t.In(cst).Format("Jan 2, 3:04 PM")
}

// timeAgo returns a human-readable relative time like "23 minutes ago".
func timeAgo(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}
