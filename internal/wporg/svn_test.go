package wporg

import (
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestParseSVNXML(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<lists>
<list path="https://plugins.svn.wordpress.org">
<entry kind="dir">
  <name>akismet</name>
  <commit revision="123">
    <date>2026-01-15T12:30:00.000000Z</date>
  </commit>
</entry>
<entry kind="dir">
  <name>jetpack</name>
  <commit revision="456">
    <date>2025-06-20T08:00:00.000000Z</date>
  </commit>
</entry>
<entry kind="file">
  <name>README</name>
</entry>
</list>
</lists>`

	var entries []SVNEntry
	err := parseSVNXML(context.Background(), strings.NewReader(xml), func(e SVNEntry) error {
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
	if entries[0].LastCommitted == nil {
		t.Fatal("first entry has nil LastCommitted")
	}
	if entries[0].LastCommitted.Year() != 2026 {
		t.Errorf("first entry year = %d, want 2026", entries[0].LastCommitted.Year())
	}

	if entries[1].Slug != "jetpack" {
		t.Errorf("second entry slug = %q, want jetpack", entries[1].Slug)
	}
}

func TestParseSVNXML_SkipsFiles(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<lists><list>
<entry kind="file"><name>README</name></entry>
<entry kind="dir"><name>plugin-a</name><commit><date>2026-01-01T00:00:00Z</date></commit></entry>
</list></lists>`

	var entries []SVNEntry
	err := parseSVNXML(context.Background(), strings.NewReader(xml), func(e SVNEntry) error {
		entries = append(entries, e)
		return nil
	}, slog.Default())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 dir entry, got %d", len(entries))
	}
}

func TestParseSVNXML_ContextCancelled(t *testing.T) {
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<lists><list>
<entry kind="dir"><name>a</name><commit><date>2026-01-01T00:00:00Z</date></commit></entry>
<entry kind="dir"><name>b</name><commit><date>2026-01-01T00:00:00Z</date></commit></entry>
</list></lists>`

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := parseSVNXML(ctx, strings.NewReader(xml), func(e SVNEntry) error {
		return nil
	}, slog.Default())

	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
