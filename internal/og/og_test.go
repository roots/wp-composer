package og

import (
	"os"
	"testing"
)

func TestGeneratePackageImage(t *testing.T) {
	data := PackageData{
		DisplayName:        "WooCommerce",
		Name:               "woocommerce",
		Type:               "plugin",
		CurrentVersion:     "9.6.2",
		Description:        "An open-source eCommerce plugin for WordPress. Build any commerce solution with the customizability and flexibility of WordPress.",
		ActiveInstalls:     "5M+",
		WpPackagesInstalls: "1.2K",
	}

	pngBytes, err := GeneratePackageImage(data)
	if err != nil {
		t.Fatalf("GeneratePackageImage: %v", err)
	}

	if len(pngBytes) < 1000 {
		t.Fatalf("PNG too small: %d bytes", len(pngBytes))
	}

	// Write to temp for visual inspection
	if os.Getenv("OG_WRITE_TEST") != "" {
		_ = os.WriteFile("/tmp/og-test-package.png", pngBytes, 0o644)
		t.Logf("wrote /tmp/og-test-package.png (%d bytes)", len(pngBytes))
	}
}

func TestGeneratePackageImageLongDesc(t *testing.T) {
	data := PackageData{
		DisplayName:        "Hello Elementor",
		Name:               "hello-elementor",
		Type:               "theme",
		CurrentVersion:     "3.4.6",
		Description:        "Hello Elementor is a lightweight and minimalist WordPress theme that was built specifically to work seamlessly with the Elementor site builder plugin. The theme is free, open-source, and designed for users who want a flexible, easy-to-use, and customizable website. The theme, which is optimized for performance, provides a solid foundation for users to build their own unique designs using the Elementor drag-and-drop site builder.",
		ActiveInstalls:     "1.0M",
		WpPackagesInstalls: "0",
	}

	pngBytes, err := GeneratePackageImage(data)
	if err != nil {
		t.Fatalf("GeneratePackageImage: %v", err)
	}

	if os.Getenv("OG_WRITE_TEST") != "" {
		_ = os.WriteFile("/tmp/og-test-long.png", pngBytes, 0o644)
		t.Logf("wrote /tmp/og-test-long.png (%d bytes)", len(pngBytes))
	}
}

func TestGenerateFallbackImage(t *testing.T) {
	pngBytes, err := GenerateFallbackImage()
	if err != nil {
		t.Fatalf("GenerateFallbackImage: %v", err)
	}

	if len(pngBytes) < 1000 {
		t.Fatalf("PNG too small: %d bytes", len(pngBytes))
	}

	if os.Getenv("OG_WRITE_TEST") != "" {
		_ = os.WriteFile("/tmp/og-test-fallback.png", pngBytes, 0o644)
		t.Logf("wrote /tmp/og-test-fallback.png (%d bytes)", len(pngBytes))
	}
}
