package wporg

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseSVNHTML(t *testing.T) {
	html := `<html><head><title> - Revision 123: /</title></head>
<body>
<h2> - Revision 123: /</h2>
<ul>
<li><a href="akismet/">akismet/</a></li>
<li><a href="jetpack/">jetpack/</a></li>
</ul>
</body></html>`

	var entries []SVNEntry
	result, err := parseSVNHTML(context.Background(), strings.NewReader(html), func(e SVNEntry) error {
		entries = append(entries, e)
		return nil
	}, slog.Default())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	if entries[0].Slug != "akismet" {
		t.Errorf("first entry slug = %q, want akismet", entries[0].Slug)
	}
	if entries[1].Slug != "jetpack" {
		t.Errorf("second entry slug = %q, want jetpack", entries[1].Slug)
	}

	if result.Revision != 123 {
		t.Errorf("revision = %d, want 123", result.Revision)
	}
}

func TestParseSVNHTML_SkipsNonEntries(t *testing.T) {
	html := `<html><body><ul>
<li><a href="../">..</a></li>
<li><a href="plugin-a/">plugin-a/</a></li>
</ul></body></html>`

	var entries []SVNEntry
	_, err := parseSVNHTML(context.Background(), strings.NewReader(html), func(e SVNEntry) error {
		entries = append(entries, e)
		return nil
	}, slog.Default())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
}

func TestParseSVNHTML_ContextCancelled(t *testing.T) {
	html := `<html><body><ul>
<li><a href="a/">a/</a></li>
<li><a href="b/">b/</a></li>
</ul></body></html>`

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := parseSVNHTML(ctx, strings.NewReader(html), func(e SVNEntry) error {
		return nil
	}, slog.Default())

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestParseSVNRevision(t *testing.T) {
	tests := []struct {
		line string
		want int64
	}{
		{`<title> - Revision 3483213: /</title>`, 3483213},
		{`<h2> - Revision 3483213: /</h2>`, 3483213},
		{`<title>Revision 999: /</title>`, 999},
		{`<li><a href="akismet/">akismet/</a></li>`, 0},
		{`no revision here`, 0},
	}

	for _, tt := range tests {
		got := parseSVNRevision(tt.line)
		if got != tt.want {
			t.Errorf("parseSVNRevision(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}

func TestParseSVNLogSlugs(t *testing.T) {
	xml := `<?xml version="1.0" encoding="utf-8"?>
<S:log-report xmlns:S="svn:" xmlns:D="DAV:">
<S:log-item>
<D:version-name>100</D:version-name>
<S:date>2026-03-15T10:00:00.000000Z</S:date>
<S:modified-path node-kind="file">/akismet/trunk/akismet.php</S:modified-path>
<S:added-path node-kind="dir">/akismet/tags/5.0</S:added-path>
</S:log-item>
<S:log-item>
<D:version-name>101</D:version-name>
<S:date>2026-03-15T11:00:00.000000Z</S:date>
<S:modified-path node-kind="file">/jetpack/trunk/jetpack.php</S:modified-path>
<S:modified-path node-kind="file">/akismet/trunk/readme.txt</S:modified-path>
</S:log-item>
</S:log-report>`

	slugs, err := parseSVNLogSlugs([]byte(xml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	slugMap := make(map[string]bool)
	for _, s := range slugs {
		slugMap[s] = true
	}

	if len(slugMap) != 2 {
		t.Fatalf("expected 2 unique slugs, got %d: %v", len(slugMap), slugs)
	}
	if !slugMap["akismet"] {
		t.Error("expected akismet in slugs")
	}
	if !slugMap["jetpack"] {
		t.Error("expected jetpack in slugs")
	}
}

func TestSlugFromPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/akismet/trunk/akismet.php", "akismet"},
		{"/jetpack/tags/1.0/jetpack.php", "jetpack"},
		{"/my-plugin/trunk/readme.txt", "my-plugin"},
		{"akismet/trunk/file.php", "akismet"},
		{"/", ""},
		{"", ""},
		{"..", ""},
	}

	for _, tt := range tests {
		got := slugFromPath(tt.path)
		if got != tt.want {
			t.Errorf("slugFromPath(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}
