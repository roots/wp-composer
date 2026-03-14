package wporg

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseSVNHTML(t *testing.T) {
	html := `<html><head><title>Revision 123: /</title></head>
<body>
<h2>Revision 123: /</h2>
<ul>
<li><a href="akismet/">akismet/</a></li>
<li><a href="jetpack/">jetpack/</a></li>
</ul>
</body></html>`

	var entries []SVNEntry
	err := parseSVNHTML(context.Background(), strings.NewReader(html), func(e SVNEntry) error {
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
}

func TestParseSVNHTML_SkipsNonEntries(t *testing.T) {
	html := `<html><body><ul>
<li><a href="../">..</a></li>
<li><a href="plugin-a/">plugin-a/</a></li>
</ul></body></html>`

	var entries []SVNEntry
	err := parseSVNHTML(context.Background(), strings.NewReader(html), func(e SVNEntry) error {
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

	err := parseSVNHTML(ctx, strings.NewReader(html), func(e SVNEntry) error {
		return nil
	}, slog.Default())

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
