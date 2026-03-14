package version

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.0", "1.0"},
		{"1.0.0", "1.0.0"},
		{"1.0.0.0", "1.0.0.0"},
		{"5.3.2", "5.3.2"},
		{"trunk", "dev-trunk"},
		{"Trunk", "dev-trunk"},
		{"TRUNK", "dev-trunk"},
		{"1.0-beta1", "1.0-beta1"},
		{"1.0-RC2", "1.0-RC2"},
		{"1.0-alpha", "1.0-alpha"},
		{"2.0.0-beta.1", "2.0.0-beta.1"},

		// Invalid
		{"", ""},
		{"  ", ""},
		{"stable", ""},
		{"1.0.0.0.1", ""},     // 5+ parts
		{"not a version", ""}, // spaces
		{"v1.0", ""},          // leading v
		{"latest", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := Normalize(tt.input)
			if got != tt.want {
				t.Errorf("Normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsValid(t *testing.T) {
	valid := []string{"1.0", "1.0.0", "1.0.0.0", "trunk", "1.0-beta1", "1.0-RC2"}
	for _, v := range valid {
		if !IsValid(v) {
			t.Errorf("IsValid(%q) = false, want true", v)
		}
	}

	invalid := []string{"", "stable", "1.0.0.0.1", "not valid", "v1.0"}
	for _, v := range invalid {
		if IsValid(v) {
			t.Errorf("IsValid(%q) = true, want false", v)
		}
	}
}

func TestNormalizeVersions(t *testing.T) {
	input := map[string]string{
		"1.0":   "https://example.com/1.0.zip",
		"2.0":   "https://example.com/2.0.zip",
		"trunk": "https://example.com/trunk.zip",
		"":      "https://example.com/empty.zip",
		"bad!":  "https://example.com/bad.zip",
	}

	got := NormalizeVersions(input)
	if len(got) != 3 {
		t.Fatalf("NormalizeVersions returned %d entries, want 3", len(got))
	}
	if got["1.0"] != "https://example.com/1.0.zip" {
		t.Error("missing 1.0")
	}
	if got["dev-trunk"] != "https://example.com/trunk.zip" {
		t.Error("missing dev-trunk")
	}
}
