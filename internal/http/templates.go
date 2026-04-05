package http

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/CloudyKit/jet/v6"
	"github.com/roots/wp-packages/internal/telemetry"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed all:static
var staticFS embed.FS

// embedLoader implements jet.Loader for an embed.FS.
type embedLoader struct {
	fs     embed.FS
	prefix string // e.g. "templates"
}

func (l *embedLoader) Exists(templatePath string) bool {
	name := l.prefix + templatePath // templatePath has leading /
	f, err := l.fs.Open(name)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func (l *embedLoader) Open(templatePath string) (io.ReadCloser, error) {
	name := l.prefix + templatePath
	return l.fs.Open(name)
}

// assetHashes maps static file paths (e.g. "assets/styles/app.css") to a
// short content hash computed once at startup from the embedded filesystem.
var assetHashes = func() map[string]string {
	hashes := make(map[string]string)
	_ = fs.WalkDir(staticFS, "static", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := staticFS.ReadFile(path)
		if err != nil {
			return nil
		}
		h := sha256.Sum256(data)
		// strip "static/" prefix to match URL paths
		key := strings.TrimPrefix(path, "static/")
		hashes[key] = hex.EncodeToString(h[:])[:12]
		return nil
	})
	return hashes
}()

// assetPath inserts a content hash into the filename for cache busting.
// e.g. "/assets/styles/app.css" → "/assets/styles/app.a1b2c3d4e5f6.css"
func assetPath(path string) string {
	key := strings.TrimPrefix(path, "/")
	v, ok := assetHashes[key]
	if !ok {
		return path
	}
	ext := filepath.Ext(path)
	return path[:len(path)-len(ext)] + "." + v + ext
}

func loadTemplates(env string) *jet.Set {
	loader := &embedLoader{fs: templateFS, prefix: "templates"}
	var opts []jet.Option
	if env != "production" {
		opts = append(opts, jet.InDevelopmentMode())
	}
	set := jet.NewSet(loader, opts...)

	// Plain functions
	set.AddGlobal("assetPath", assetPath)
	set.AddGlobal("formatNumber", formatNumber)
	set.AddGlobal("formatNumberComma", formatNumberComma)
	set.AddGlobal("paginate", paginateURL)
	set.AddGlobal("paginatePartial", paginatePartialURL)
	set.AddGlobal("untaggedPaginate", untaggedPaginateURL)
	set.AddGlobal("untaggedPaginateP", untaggedPaginatePartialURL)
	set.AddGlobal("formatCST", formatCST)
	set.AddGlobal("timeAgo", timeAgo)
	set.AddGlobal("timeAgoShort", timeAgoShort)
	set.AddGlobal("formatDuration", formatDuration)
	set.AddGlobal("pageRange", pageRange)
	set.AddGlobal("pct", func(n, total int64) string {
		if total == 0 {
			return "0"
		}
		return fmt.Sprintf("%.1f", float64(n)*100/float64(total))
	})
	set.AddGlobal("wporgURL", func(composerName string) string {
		parts := strings.SplitN(composerName, "/", 2)
		if len(parts) != 2 {
			return "https://wordpress.org/"
		}
		section := "plugins"
		if parts[0] == "wp-theme" {
			section = "themes"
		}
		return "https://wordpress.org/" + section + "/" + parts[1] + "/"
	})
	set.AddGlobal("isProduction", func() bool { return env == "production" })

	// Functions returning raw HTML — use |raw in templates to bypass escaping
	set.AddGlobal("jsonLD", renderJsonLD)
	set.AddGlobal("installChart", renderInstallChart)

	return set
}

func render(w http.ResponseWriter, r *http.Request, set *jet.Set, name string, data map[string]any) {
	vars := make(jet.VarMap)
	// Defaults for variables used in layout that not every handler passes.
	vars.Set("CDNURL", "")
	vars.Set("AppURL", "")
	vars.Set("OGImage", "")
	vars.Set("Gone", false)
	for k, v := range data {
		vars.Set(k, v)
	}
	vars.Set("Path", r.URL.Path)

	tmpl, err := set.GetTemplate(name)
	if err != nil {
		captureError(r, err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.Execute(w, vars, nil); err != nil {
		captureError(r, err)
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

func formatNumberComma(n int64) string {
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

// formatAxisLabel formats a number for chart Y-axis labels, keeping enough
// precision to avoid collisions (e.g. 1500 → "1.5K" instead of "2K").
func formatAxisLabel(n int) string {
	if n >= 1_000_000 {
		v := float64(n) / 1_000_000
		if v == float64(int(v)) {
			return fmt.Sprintf("%dM", int(v))
		}
		return fmt.Sprintf("%.1fM", v)
	}
	if n >= 1_000 {
		v := float64(n) / 1_000
		if v == float64(int(v)) {
			return fmt.Sprintf("%dK", int(v))
		}
		return fmt.Sprintf("%.1fK", v)
	}
	return fmt.Sprintf("%d", n)
}

type paginationPage struct {
	Number     int
	URL        string
	PartialURL string
}

type pagination struct {
	Page        int
	TotalPages  int
	Target      string
	SwapTarget  string
	PrevURL     string
	PrevPartial string
	NextURL     string
	NextPartial string
	Pages       []paginationPage
}

func buildPagination(page, totalPages int, target, swapTarget string, urlFn, partialFn func(int) string) *pagination {
	if totalPages <= 1 {
		return nil
	}
	p := &pagination{
		Page:       page,
		TotalPages: totalPages,
		Target:     target,
		SwapTarget: swapTarget,
	}
	if page > 1 {
		p.PrevURL = urlFn(page - 1)
		p.PrevPartial = partialFn(page - 1)
	}
	if page < totalPages {
		p.NextURL = urlFn(page + 1)
		p.NextPartial = partialFn(page + 1)
	}
	for _, n := range pageRange(page, totalPages) {
		pg := paginationPage{Number: n}
		if n > 0 {
			pg.URL = urlFn(n)
			pg.PartialURL = partialFn(n)
		}
		p.Pages = append(p.Pages, pg)
	}
	return p
}

type publicFilters struct {
	Search string
	Type   string
	Sort   string
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

func untaggedPaginateURL(filter, search, author, sort string, page int) string {
	v := url.Values{}
	if filter != "" {
		v.Set("filter", filter)
	}
	if search != "" {
		v.Set("search", search)
	}
	if author != "" {
		v.Set("author", author)
	}
	if sort != "" && sort != "active_installs" {
		v.Set("sort", sort)
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	q := v.Encode()
	if q == "" {
		return "/untagged"
	}
	return "/untagged?" + q
}

func untaggedPaginatePartialURL(filter, search, author, sort string, page int) string {
	v := url.Values{}
	if filter != "" {
		v.Set("filter", filter)
	}
	if search != "" {
		v.Set("search", search)
	}
	if author != "" {
		v.Set("author", author)
	}
	if sort != "" && sort != "active_installs" {
		v.Set("sort", sort)
	}
	if page > 1 {
		v.Set("page", fmt.Sprintf("%d", page))
	}
	q := v.Encode()
	if q == "" {
		return "/untagged-partial"
	}
	return "/untagged-partial?" + q
}

func renderJsonLD(data any) string {
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
		return out
	}
	b, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return `<script type="application/ld+json">` + string(b) + `</script>`
}

var cst = func() *time.Location {
	loc, err := time.LoadLocation("America/Chicago")
	if err != nil {
		return time.FixedZone("CST", -6*60*60)
	}
	return loc
}()

// formatCST converts an RFC3339 or "2006-01-02 15:04:05" string to "Jan 2, 3:04 PM" in America/Chicago.
func formatCST(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", raw)
	}
	if err != nil {
		return raw
	}
	return t.In(cst).Format("Jan 2, 3:04 PM")
}

// formatDuration converts seconds (as *int) to a human-readable duration like "2m 35s".
func formatDuration(v *int) string {
	if v == nil {
		return ""
	}
	s := *v
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	return fmt.Sprintf("%dm %ds", s/60, s%60)
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

// timeAgoShort is like timeAgo but shows "Jan 2006" for dates older than 30 days.
func timeAgoShort(raw string) string {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	if time.Since(t).Hours()/24 > 30 {
		return t.Format("Jan 2006")
	}
	return timeAgo(raw)
}

// pageRange returns page numbers to display in pagination. 0 represents an ellipsis.
// Shows current page with one neighbor on each side, plus first and last pages.
func pageRange(current, total int) []int {
	if total <= 5 {
		pages := make([]int, total)
		for i := range pages {
			pages[i] = i + 1
		}
		return pages
	}
	seen := map[int]bool{}
	var pages []int
	for _, p := range []int{1, current - 1, current, current + 1, total} {
		if p >= 1 && p <= total && !seen[p] {
			seen[p] = true
			pages = append(pages, p)
		}
	}
	// Insert ellipses where there are gaps
	var result []int
	for i, p := range pages {
		if i > 0 && p > pages[i-1]+1 {
			result = append(result, 0)
		}
		result = append(result, p)
	}
	return result
}

// renderInstallChart renders a server-side SVG bar chart for monthly install data.
func renderInstallChart(data []telemetry.MonthlyInstall) string {
	if len(data) == 0 {
		return ""
	}

	// Use last 12 months max
	if len(data) > 12 {
		data = data[len(data)-12:]
	}

	// Find max value for scaling
	max := 0
	for _, m := range data {
		if m.Installs > max {
			max = m.Installs
		}
	}
	if max == 0 {
		return ""
	}

	// Compute nice Y-axis tick values and scale to the highest tick
	ticks := yAxisTicks(max)
	if len(ticks) > 0 {
		max = ticks[len(ticks)-1]
	}

	n := len(data)
	padLeft := 44.0
	padRight := 4.0
	padTop := 20.0
	padBottom := 28.0
	chartW := 600.0
	chartH := 160.0
	totalW := padLeft + chartW + padRight
	barGap := 6.0
	maxBarW := 48.0
	barW := (chartW - float64(n-1)*barGap) / float64(n)
	if barW > maxBarW {
		barW = maxBarW
	}
	totalH := padTop + chartH + padBottom

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" width="100%%" xmlns="http://www.w3.org/2000/svg" role="img" aria-label="Monthly Composer installs chart">`, totalW, totalH)
	b.WriteString(`<style>g.bar .tip{opacity:0;transition:opacity .1s}g.bar:hover .tip{opacity:1}g.bar:hover rect{opacity:.9!important}</style>`)

	// Y-axis tick lines and labels
	for _, tick := range ticks {
		y := padTop + chartH - (float64(tick)/float64(max))*chartH
		// Grid line
		fmt.Fprintf(&b, `<line x1="%.0f" y1="%.1f" x2="%.0f" y2="%.1f" stroke="#e5e7eb" stroke-width="1"/>`,
			padLeft, y, totalW-padRight, y)
		// Label
		fmt.Fprintf(&b, `<text class="label" x="%.0f" y="%.1f" text-anchor="end" fill="#9ca3af" style="font-size:10px;font-family:sans-serif">%s</text>`,
			padLeft-6, y+3.5, formatAxisLabel(tick))
	}

	for i, m := range data {
		x := padLeft + float64(i)*(barW+barGap)
		barH := (float64(m.Installs) / float64(max)) * chartH
		y := padTop + chartH - barH

		label := formatNumberComma(int64(m.Installs))

		// Bar with rounded top corners
		radius := 3.0
		if barH < radius*2 {
			radius = barH / 2
		}

		// Wrap bar + hover label in a group
		b.WriteString(`<g class="bar">`)
		ariaLabel := fmt.Sprintf("%s: %s installs", m.Month, label)
		fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="%.1f" fill="#525ddc" style="opacity:.6;transition:opacity .15s" role="graphics-symbol" aria-label="%s"/>`,
			x, y, barW, barH, radius, htmlEscapeString(ariaLabel))
		// Hover label above bar
		tipY := y - 6
		if tipY < 8 {
			tipY = 8
		}
		fmt.Fprintf(&b, `<text class="tip tip-text" x="%.1f" y="%.1f" text-anchor="middle" fill="#525ddc" style="font-size:10px;font-weight:600;font-family:sans-serif">%s</text>`,
			x+barW/2, tipY, label)
		b.WriteString(`</g>`)

		// X-axis label
		labelX := x + barW/2
		labelY := padTop + chartH + 16
		fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" text-anchor="middle" fill="#9ca3af" style="font-size:10px;font-family:sans-serif">%s</text>`,
			labelX, labelY, m.Month)
	}

	b.WriteString(`</svg>`)
	return b.String()
}

// htmlEscapeString escapes special HTML characters in a string.
func htmlEscapeString(s string) string {
	replacer := strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&quot;",
		"'", "&#39;",
	)
	return replacer.Replace(s)
}

// yAxisTicks returns 3-5 nice round tick values from 0 to max.
func yAxisTicks(max int) []int {
	if max <= 0 {
		return nil
	}
	// Find a nice step: 1, 2, 5, 10, 20, 50, 100, ...
	target := max / 4
	if target < 1 {
		target = 1
	}
	mag := 1
	for mag*10 <= target {
		mag *= 10
	}
	var step int
	if mag*2 >= target {
		step = mag * 2
	} else if mag*5 >= target {
		step = mag * 5
	} else {
		step = mag * 10
	}

	var ticks []int
	for v := step; v < max; v += step {
		ticks = append(ticks, v)
	}
	// Always include a tick at or above max so bars don't exceed the top grid line
	topTick := ((max + step - 1) / step) * step
	ticks = append(ticks, topTick)
	return ticks
}
