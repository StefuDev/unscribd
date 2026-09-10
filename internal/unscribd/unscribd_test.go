package unscribd

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocumentIDAndJSONP(t *testing.T) {
	id, err := ExtractDocumentID("https://www.scribd.com/document/123/example")
	if err != nil || id != "123" {
		t.Fatalf("got %q, %v", id, err)
	}
	id, err = ExtractDocumentID("https://www.scribd.com/doc/456/48")
	if err != nil || id != "456" {
		t.Fatalf("legacy URL got %q, %v", id, err)
	}
	page, err := ParseJSONP(`window.page1_callback(["<div>ok</div>"]);`)
	if err != nil || page != "<div>ok</div>" {
		t.Fatalf("got %q, %v", page, err)
	}
}

func TestFilenameAndTextPDF(t *testing.T) {
	if got := SanitizeFilename(` My/Document `); got != "My_Document" {
		t.Fatalf("got %q", got)
	}
	path := filepath.Join(t.TempDir(), "text.pdf")
	if err := WriteTextPDF(path, "Title", []string{"Hello world"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data[:8]) != "%PDF-1.4" {
		t.Fatal("missing PDF header")
	}
}

func TestSearchableImagePDFAddsVisibleSelectableText(t *testing.T) {
	directory := t.TempDir()
	imagePath, pdfPath := filepath.Join(directory, "page.jpg"), filepath.Join(directory, "page.pdf")
	f, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := WriteSearchableImagePDF(pdfPath, []string{imagePath}, []string{"hello searchable text"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "3 Tr") || !strings.Contains(string(data), "0 0 0 rg") || !strings.Contains(string(data), "hello searchable text") {
		t.Fatal("missing visible text overlay")
	}
}

func TestPageTextPositionsMatchScribdMarkup(t *testing.T) {
	page := extractPage(1, `<div class="newpage" style="width: 900px; height: 1200px"><div class="ff2" style="font-size: 10px"><span style="left: 20px; top: 30px">Hello</span></div></div>`)
	if page.Width != 900 || page.Height != 1200 {
		t.Fatalf("got page size %dx%d", page.Width, page.Height)
	}
	if len(page.TextItems) != 1 {
		t.Fatalf("got %d text items", len(page.TextItems))
	}
	item := page.TextItems[0]
	if item.Text != "Hello" || item.Left != 20 || item.Top != 30 || item.FontSize != 10 || item.Family != "ff2" {
		t.Fatalf("unexpected item: %#v", item)
	}
}

func TestNestedScribdSpansKeepCompleteLine(t *testing.T) {
	page := extractPage(1, `<div style="width: 902px; height:1166px"><div class="ff9" style="font-size:131px"><span class=a style="left:212px;top:894px">2) inc x 8 <span class=l6>(16)</span></span></div></div>`)
	if len(page.TextItems) != 1 || page.TextItems[0].Text != "2) inc x 8 (16)" {
		t.Fatalf("nested span text was truncated: %#v", page.TextItems)
	}
}

func TestGroupedPositionedSpansInheritCoordinates(t *testing.T) {
	page := extractPage(1, `<div style="width:902px;height:1439px"><div class="ff45" style="font-size:262px"><span class="g" style="top:541px"><span class="a" style="left:1413px;color:#ffc397">Nick</span></span></div></div>`)
	if len(page.TextItems) != 1 {
		t.Fatalf("got %d text items", len(page.TextItems))
	}
	item := page.TextItems[0]
	if item.Text != "Nick" || item.Left != 1413 || item.Top != 541 || item.FontSize != 262 {
		t.Fatalf("group coordinate was not inherited: %#v", item)
	}
}

func TestTextSpacingMatchesScribdCSS(t *testing.T) {
	page := extractPage(1, `<div style="width:902px;height:1166px"><div class="ff2" style="font-size:103px"><span class=a style="left:785px;top:946px;word-spacing:-66px;letter-spacing:61px">MR-magic ring</span></div></div>`)
	if len(page.TextItems) != 1 || page.TextItems[0].LetterSpacing != 61 || page.TextItems[0].WordSpacing != -66 {
		t.Fatalf("spacing was not preserved: %#v", page.TextItems)
	}
}

func TestGlyphOffsetsReplayScribdNestedSpans(t *testing.T) {
	glyphs := parseGlyphs(`M<span class="l" style="margin-left:-60px">R<span class="w" style="width:377px"></span>-</span>`)
	if len(glyphs) != 3 || glyphs[0].Text != "M" || glyphs[1].Before != -60 || glyphs[2].Text != "-" || glyphs[2].Before != 377 {
		t.Fatalf("unexpected glyph offsets: %#v", glyphs)
	}
}

func TestGlyphOffsetsReplayScribdClassSpacing(t *testing.T) {
	glyphs := parseGlyphs(`@<span class="l10">d<span class="l9">a</span></span><span class="w6"></span>b`)
	if len(glyphs) != 4 || glyphs[1].Before != -10 || glyphs[2].Before != -9 || glyphs[3].Before != 6 {
		t.Fatalf("unexpected class glyph offsets: %#v", glyphs)
	}
}

func TestNestedGlyphRunKeepsWordSpacing(t *testing.T) {
	page := extractPage(1, `<div style="width:901px;height:1186px"><div class="ff1" style="font-size:120px"><span class="a" style="left:1119px;top:2801px;word-spacing:406px;letter-spacing:19px">M<span class="l" style="margin-left:-19px">a</span> <span class="w" style="width:403px"></span>A</span></div></div>`)
	if len(page.TextItems) != 1 || page.TextItems[0].WordSpacing != 406 || len(page.TextItems[0].Glyphs) != 4 {
		t.Fatalf("word spacing was not retained: %#v", page.TextItems)
	}
}

func TestPlainTextRunKeepsCSSSpacing(t *testing.T) {
	page := extractPage(1, `<div style="width:901px;height:1186px"><div class="ff1" style="font-size:94px"><span class="a" style="left:2552px;top:3111px;word-spacing:-2px">mr: magic ring</span></div></div>`)
	if len(page.TextItems) != 1 || page.TextItems[0].WordSpacing != -2 || len(page.TextItems[0].Glyphs) != 0 {
		t.Fatalf("plain CSS spacing was not retained: %#v", page.TextItems)
	}
}

func TestTextItemPreservesCSSOpacity(t *testing.T) {
	page := extractPage(1, `<div style="width:901px;height:1186px"><div class="ff1" style="font-size:94px"><span class="a" style="left:2552px;top:3111px;opacity:0.50">faint source overlay</span></div></div>`)
	if len(page.TextItems) != 1 || page.TextItems[0].Opacity != .5 {
		t.Fatalf("opacity was not retained: %#v", page.TextItems)
	}
	if got := itemOpacity(TextItem{}); got != 1 {
		t.Fatalf("zero-value opacity should remain visible, got %v", got)
	}
}

func TestEncodedGlyphRunsDoNotUseDashAlignment(t *testing.T) {
	if !encodedGlyphRun(`1;A:3 2: 5-0 2 %&-8 <:`) {
		t.Fatal("subset-font glyph stream was not detected")
	}
	if encodedGlyphRun(`MR - magic ring`) {
		t.Fatal("readable text must remain eligible for dash alignment")
	}
}

func TestVisibleTextPreservesNonBreakingSpace(t *testing.T) {
	if got := visibleText("&nbsp;(2sc, dec)"); got != "\u00a0(2sc, dec)" {
		t.Fatalf("layout space was removed: %q", got)
	}
}

func TestComposePageImageUsesDeclaredCanvas(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.png")
	f, err := os.Create(source)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	page := Page{Number: 1, Width: 4, Height: 4, HTML: `<img class="absimg" style="left:0px;top:0px;clip:rect(0px 4px 4px 0px)" orig="https://example.test/source.png">`}
	output := composePageImage(page, map[string]string{"https://example.test/source.png": source}, directory)
	if output == "" {
		t.Fatal("expected composed page")
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width != 4 || config.Height != 4 {
		t.Fatalf("got composed dimensions %dx%d, err=%v", config.Width, config.Height, err)
	}
}

func TestPDFColorFiltersScribdAntiScrapeWhite(t *testing.T) {
	if _, _, _, keep := pdfColor("#fffeff"); keep {
		t.Fatal("near-white anti-scrape text should be filtered")
	}
	r, g, b, keep := pdfColor("#b8101b")
	if !keep || r < .7 || g > .1 || b > .2 {
		t.Fatalf("unexpected color: %.2f %.2f %.2f keep=%v", r, g, b, keep)
	}
}

func TestDiskCacheRoundTrip(t *testing.T) {
	directory := t.TempDir()
	source := filepath.Join(directory, "source.bin")
	destination := filepath.Join(directory, "destination.bin")
	if err := os.WriteFile(source, []byte("cached bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	cachePut(directory, "test-key", source, 1<<20)
	if !cacheGet(directory, "test-key", destination) {
		t.Fatal("expected cache hit")
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "cached bytes" {
		t.Fatalf("cached content mismatch: %q, %v", data, err)
	}
}

func TestDirectPDFUsesPositionedImageTiles(t *testing.T) {
	directory := t.TempDir()
	imagePath := filepath.Join(directory, "tile.jpg")
	file, err := os.Create(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	url := "https://example.test/tile.jpg"
	page := Page{Number: 1, Width: 4, Height: 4, HTML: `<img class="absimg" style="left:-1px;top:-1px;clip:rect(1px 5px 5px 1px)" orig="` + url + `">`}
	pdfPath := filepath.Join(directory, "direct.pdf")
	if err := WriteDirectPositionedFontPDF(pdfPath, []Page{page}, []map[string]string{{url: imagePath}}, []string{""}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(pdfPath)
	if err != nil || info.Size() == 0 {
		t.Fatalf("direct PDF was not written: %v", err)
	}
}

func TestMultiTilePageUsesRasterFallback(t *testing.T) {
	page := Page{Number: 1, HTML: `<img class="absimg" style="left:0px;top:0px;clip:rect(0px 4px 4px 0px)" orig="https://example.test/a.png"><img class="absimg" style="left:4px;top:0px;clip:rect(0px 4px 4px 0px)" orig="https://example.test/a.png">`}
	if !needsRasterFallback(page, map[string]string{"https://example.test/a.png": "missing.png"}) {
		t.Fatal("multi-tile pages must retain the lossless raster compositor")
	}
}
