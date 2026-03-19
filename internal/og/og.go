package og

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	"github.com/fogleman/gg"
	"github.com/golang/freetype/truetype"
	"golang.org/x/image/font"
)

//go:embed fonts/*.ttf
var fontsFS embed.FS

//go:embed roots-icon.png
var rootsIconData []byte

const (
	Width  = 1200
	Height = 630
)

// Brand colors matching Tailwind config in layout.html
var (
	colorBrandPrimary = color.RGBA{R: 0x52, G: 0x5d, B: 0xdc, A: 0xff}
	colorGray900      = color.RGBA{R: 0x11, G: 0x18, B: 0x27, A: 0xff}
	colorGray500      = color.RGBA{R: 0x6b, G: 0x72, B: 0x80, A: 0xff}
	colorGray400      = color.RGBA{R: 0x9c, G: 0xa3, B: 0xaf, A: 0xff}
	colorGray200      = color.RGBA{R: 0xe5, G: 0xe7, B: 0xeb, A: 0xff}
	colorGray100      = color.RGBA{R: 0xf3, G: 0xf4, B: 0xf6, A: 0xff}
	colorWhite        = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
)

// PackageData holds the data needed to render an OG image.
type PackageData struct {
	DisplayName        string
	Name               string
	Type               string // "plugin" or "theme"
	CurrentVersion     string
	Description        string
	ActiveInstalls     string // pre-formatted, e.g. "1.2M"
	WpPackagesInstalls string // pre-formatted, e.g. "350"
}

var (
	fontSansBold    *truetype.Font
	fontSansMedium  *truetype.Font
	fontSansRegular *truetype.Font
	fontMonoRegular *truetype.Font
	fontMonoMedium  *truetype.Font
	rootsIcon       image.Image
)

func init() {
	fontSansBold = mustLoadFont("fonts/PublicSans-Bold.ttf")
	fontSansMedium = mustLoadFont("fonts/PublicSans-Medium.ttf")
	fontSansRegular = mustLoadFont("fonts/PublicSans-Regular.ttf")
	fontMonoRegular = mustLoadFont("fonts/JetBrainsMono-Regular.ttf")
	fontMonoMedium = mustLoadFont("fonts/JetBrainsMono-Medium.ttf")

	img, err := png.Decode(bytes.NewReader(rootsIconData))
	if err != nil {
		panic(fmt.Sprintf("decoding roots icon: %v", err))
	}
	rootsIcon = img
}

func mustLoadFont(path string) *truetype.Font {
	data, err := fontsFS.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("loading font %s: %v", path, err))
	}
	f, err := truetype.Parse(data)
	if err != nil {
		panic(fmt.Sprintf("parsing font %s: %v", path, err))
	}
	return f
}

func fontFace(f *truetype.Font, size float64) font.Face {
	return truetype.NewFace(f, &truetype.Options{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingFull,
	})
}

// drawTextVCenter draws text vertically centered at centerY using font ascent metrics.
// This gives true visual centering unlike DrawStringAnchored which includes descenders.
func drawTextVCenter(dc *gg.Context, text string, x, centerY float64) {
	m := dc.FontHeight()
	// Ascent is roughly 70% of total height for most fonts
	ascent := m * 0.72
	dc.DrawString(text, x, centerY+ascent/2)
}

// GeneratePackageImage renders an OG image for a package and returns PNG bytes.
func GeneratePackageImage(pkg PackageData) ([]byte, error) {
	dc := gg.NewContext(Width, Height)

	// Background — light gray
	dc.SetColor(colorGray100)
	dc.Clear()

	// Card — white rounded rectangle centered with shadow-like border
	cardX, cardY := 60.0, 40.0
	cardW, cardH := float64(Width)-120.0, float64(Height)-80.0
	drawRoundedRect(dc, cardX, cardY, cardW, cardH, 20)
	dc.SetColor(colorGray200)
	dc.SetLineWidth(1)
	dc.StrokePreserve()
	dc.SetColor(colorWhite)
	dc.Fill()

	// Content padding within card
	cx := cardX + 48
	contentMaxW := cardW - 96
	y := cardY + 52

	// --- "WP Packages" header ---
	dc.SetFontFace(fontFace(fontSansBold, 18))
	dc.SetColor(colorBrandPrimary)
	dc.DrawString("WP Packages", cx, y)
	y += 44

	// --- Package name (large, bold) ---
	title := pkg.DisplayName
	if title == "" {
		title = pkg.Name
	}
	dc.SetFontFace(fontFace(fontSansBold, 34))
	dc.SetColor(colorGray900)
	title = truncateText(dc, title, contentMaxW)
	dc.DrawString(title, cx, y)
	y += 32

	// --- Version + type badge ---
	lineCenterY := y + 10
	if pkg.CurrentVersion != "" {
		dc.SetFontFace(fontFace(fontMonoRegular, 15))
		dc.SetColor(colorGray500)
		vStr := "v" + pkg.CurrentVersion
		drawTextVCenter(dc, vStr, cx, lineCenterY)
		vw, _ := dc.MeasureString(vStr)
		drawTypeBadge(dc, cx+vw+10, lineCenterY, pkg.Type)
	} else {
		drawTypeBadge(dc, cx, lineCenterY, pkg.Type)
	}
	y += 36

	// --- Composer require line ---
	codeH := 40.0
	drawRoundedRect(dc, cx, y, contentMaxW, codeH, 8)
	dc.SetColor(colorGray100)
	dc.Fill()

	codeCenterY := y + codeH/2
	dc.SetFontFace(fontFace(fontMonoMedium, 15))
	dc.SetColor(colorGray400)
	drawTextVCenter(dc, "$", cx+14, codeCenterY)
	dc.SetFontFace(fontFace(fontMonoRegular, 15))
	dc.SetColor(colorGray500)
	requireStr := fmt.Sprintf("composer require wp-%s/%s", pkg.Type, pkg.Name)
	requireStr = truncateText(dc, requireStr, contentMaxW-60)
	drawTextVCenter(dc, requireStr, cx+32, codeCenterY)
	y += codeH + 38

	// --- Description ---
	if pkg.Description != "" {
		dc.SetFontFace(fontFace(fontSansRegular, 19))
		dc.SetColor(colorGray500)
		desc := wrapAndTruncate(dc, pkg.Description, contentMaxW, 4)
		for i, line := range desc {
			dc.DrawString(line, cx, y+float64(i)*30)
		}
	}

	// --- Bottom stats bar ---
	// Divider line
	dividerY := cardY + cardH - 60
	dc.SetColor(colorGray200)
	dc.SetLineWidth(1)
	dc.DrawLine(cx, dividerY, cx+contentMaxW, dividerY)
	dc.Stroke()

	// Footer center line — midpoint between divider and card bottom
	footerCenterY := dividerY + (cardY+cardH-dividerY)/2

	// Active installs — icon vertically centered with text
	dc.SetFontFace(fontFace(fontSansRegular, 15))
	dc.SetColor(colorGray400)
	drawPeopleIcon(dc, cx, footerCenterY-5, 14, colorGray400)
	dc.SetFontFace(fontFace(fontSansRegular, 15))
	dc.SetColor(colorGray400)
	drawTextVCenter(dc, pkg.ActiveInstalls+" active installs", cx+22, footerCenterY)

	// Composer installs
	composerX := cx + 260.0
	drawTerminalIcon(dc, composerX, footerCenterY-7, 14, colorGray400)
	dc.SetFontFace(fontFace(fontSansRegular, 15))
	dc.SetColor(colorGray400)
	drawTextVCenter(dc, pkg.WpPackagesInstalls+" composer installs", composerX+22, footerCenterY)

	// "by [icon] roots.io" (bottom right)
	drawByRootsFooter(dc, cx+contentMaxW, footerCenterY, 16, 15)

	// Encode to PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// GenerateFallbackImage creates a generic branded OG image.
func GenerateFallbackImage() ([]byte, error) {
	dc := gg.NewContext(Width, Height)

	// Background
	dc.SetColor(colorGray100)
	dc.Clear()

	// Card
	cardX, cardY := 60.0, 40.0
	cardW, cardH := float64(Width)-120.0, float64(Height)-80.0
	drawRoundedRect(dc, cardX, cardY, cardW, cardH, 20)
	dc.SetColor(colorGray200)
	dc.SetLineWidth(1)
	dc.StrokePreserve()
	dc.SetColor(colorWhite)
	dc.Fill()

	// Roots icon centered
	centerX, centerY := float64(Width)/2, float64(Height)/2-40
	logoSize := 88.0
	drawScaledImage(dc, rootsIcon, centerX-logoSize/2, centerY-logoSize/2, logoSize, logoSize)

	// Title
	dc.SetFontFace(fontFace(fontSansBold, 36))
	dc.SetColor(colorGray900)
	dc.DrawStringAnchored("WP Packages", centerX, centerY+logoSize/2+32, 0.5, 0.5)

	// Subtitle
	dc.SetFontFace(fontFace(fontSansRegular, 18))
	dc.SetColor(colorGray500)
	dc.DrawStringAnchored("WordPress plugins and themes via Composer", centerX, centerY+logoSize/2+68, 0.5, 0.5)

	// "by [icon] roots.io" at bottom center
	drawByRootsFooterCentered(dc, centerX, cardY+cardH-44, 20, 17)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dc.Image()); err != nil {
		return nil, fmt.Errorf("encoding PNG: %w", err)
	}
	return buf.Bytes(), nil
}

// Helper drawing functions

func drawScaledImage(dc *gg.Context, img image.Image, x, y, w, h float64) {
	srcW := float64(img.Bounds().Dx())
	srcH := float64(img.Bounds().Dy())
	sx := w / srcW
	sy := h / srcH
	dc.Push()
	dc.Translate(x, y)
	dc.Scale(sx, sy)
	dc.DrawImage(img, 0, 0)
	dc.Pop()
}

func drawRoundedRect(dc *gg.Context, x, y, w, h, r float64) {
	dc.NewSubPath()
	dc.DrawArc(x+r, y+r, r, gg.Radians(180), gg.Radians(270))
	dc.LineTo(x+w-r, y)
	dc.DrawArc(x+w-r, y+r, r, gg.Radians(270), gg.Radians(360))
	dc.LineTo(x+w, y+h-r)
	dc.DrawArc(x+w-r, y+h-r, r, gg.Radians(0), gg.Radians(90))
	dc.LineTo(x+r, y+h)
	dc.DrawArc(x+r, y+h-r, r, gg.Radians(90), gg.Radians(180))
	dc.ClosePath()
}

func drawTypeBadge(dc *gg.Context, x, centerY float64, pkgType string) {
	dc.SetFontFace(fontFace(fontSansMedium, 13))
	tw, _ := dc.MeasureString(pkgType)
	padX := 8.0
	badgeW := tw + padX*2
	badgeH := 22.0

	drawRoundedRect(dc, x, centerY-badgeH/2, badgeW, badgeH, 5)
	dc.SetColor(colorGray100)
	dc.Fill()

	dc.SetColor(colorGray500)
	drawTextVCenter(dc, pkgType, x+padX, centerY)
}

func drawPeopleIcon(dc *gg.Context, x, y, size float64, c color.Color) {
	dc.SetColor(c)
	dc.SetLineWidth(1.5)

	// Simple two-person icon
	// Person 1 (front, larger)
	dc.DrawCircle(x+size*0.35, y-size*0.1, size*0.22)
	dc.Stroke()
	dc.DrawArc(x+size*0.35, y+size*0.75, size*0.38, gg.Radians(205), gg.Radians(335))
	dc.Stroke()

	// Person 2 (behind, smaller, offset right)
	dc.DrawCircle(x+size*0.75, y-size*0.18, size*0.18)
	dc.Stroke()
	dc.DrawArc(x+size*0.75, y+size*0.6, size*0.3, gg.Radians(210), gg.Radians(330))
	dc.Stroke()
}

func drawTerminalIcon(dc *gg.Context, x, y, size float64, c color.Color) {
	dc.SetColor(c)
	dc.SetLineWidth(1.5)

	w := size * 1.2
	h := size * 0.85

	// Terminal box (simple rectangle with rounded corners)
	r := 2.0
	dc.MoveTo(x+r, y)
	dc.LineTo(x+w-r, y)
	dc.LineTo(x+w, y+r)
	dc.LineTo(x+w, y+h-r)
	dc.LineTo(x+w-r, y+h)
	dc.LineTo(x+r, y+h)
	dc.LineTo(x, y+h-r)
	dc.LineTo(x, y+r)
	dc.ClosePath()
	dc.Stroke()

	// Prompt chevron >_
	cy := y + h*0.5
	dc.MoveTo(x+w*0.15, cy-h*0.18)
	dc.LineTo(x+w*0.35, cy)
	dc.LineTo(x+w*0.15, cy+h*0.18)
	dc.Stroke()

	// Underscore cursor
	dc.DrawLine(x+w*0.42, cy+h*0.18, x+w*0.65, cy+h*0.18)
	dc.Stroke()
}

// drawByRootsFooter draws "by [icon] roots.io" right-aligned at (rightX, centerY).
func drawByRootsFooter(dc *gg.Context, rightX, centerY, iconSize, fontSize float64) {
	dc.SetFontFace(fontFace(fontSansMedium, fontSize))

	byW, _ := dc.MeasureString("by ")
	rootsW, _ := dc.MeasureString("roots.io")
	gap := 4.0
	totalW := byW + iconSize + gap + rootsW
	startX := rightX - totalW

	dc.SetColor(colorGray400)
	drawTextVCenter(dc, "by ", startX, centerY)

	drawScaledImage(dc, rootsIcon, startX+byW, centerY-iconSize/2, iconSize, iconSize)

	dc.SetColor(colorBrandPrimary)
	dc.SetFontFace(fontFace(fontSansMedium, fontSize))
	drawTextVCenter(dc, "roots.io", startX+byW+iconSize+gap, centerY)
}

// drawByRootsFooterCentered draws "by [icon] roots.io" centered at (centerX, centerY).
func drawByRootsFooterCentered(dc *gg.Context, centerX, centerY, iconSize, fontSize float64) {
	dc.SetFontFace(fontFace(fontSansMedium, fontSize))

	byW, _ := dc.MeasureString("by ")
	rootsW, _ := dc.MeasureString("roots.io")
	gap := 4.0
	totalW := byW + iconSize + gap + rootsW
	startX := centerX - totalW/2

	dc.SetColor(colorGray400)
	drawTextVCenter(dc, "by ", startX, centerY)

	drawScaledImage(dc, rootsIcon, startX+byW, centerY-iconSize/2, iconSize, iconSize)

	dc.SetColor(colorBrandPrimary)
	dc.SetFontFace(fontFace(fontSansMedium, fontSize))
	drawTextVCenter(dc, "roots.io", startX+byW+iconSize+gap, centerY)
}

func truncateText(dc *gg.Context, text string, maxW float64) string {
	w, _ := dc.MeasureString(text)
	if w <= maxW {
		return text
	}
	for len(text) > 0 {
		text = text[:len(text)-1]
		w, _ = dc.MeasureString(text + "…")
		if w <= maxW {
			return text + "…"
		}
	}
	return "…"
}

func wrapAndTruncate(dc *gg.Context, text string, maxW float64, maxLines int) []string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}

	var lines []string
	current := words[0]
	hasMore := false

	for i, word := range words[1:] {
		test := current + " " + word
		w, _ := dc.MeasureString(test)
		if w > maxW {
			lines = append(lines, current)
			if len(lines) >= maxLines {
				hasMore = true
				break
			}
			current = word
		} else {
			current = test
		}
		if i == len(words)-2 {
			// Last word processed
			lines = append(lines, current)
		}
	}

	if !hasMore && len(lines) == 0 {
		lines = append(lines, current)
	}

	// If we hit max lines and there's more text, add ellipsis to last line
	if hasMore && len(lines) > 0 {
		last := lines[len(lines)-1]
		lines[len(lines)-1] = truncateText(dc, last+"…", maxW)
	}

	return lines
}
