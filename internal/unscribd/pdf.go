package unscribd

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/phpdave11/gofpdf"
)

func WriteImagePDF(path string, imagePaths []string) error {
	return writeImagePDF(path, imagePaths, nil, nil)
}

// WriteSearchableImagePDF adds a visible, selectable text layer to each raster
// page. Scribd serves its text separately from the artwork on many documents.
func WriteSearchableImagePDF(path string, imagePaths, texts []string) error {
	return writeImagePDF(path, imagePaths, texts, nil)
}

func WritePositionedSearchableImagePDF(path string, imagePaths []string, pages []Page) error {
	return writeImagePDF(path, imagePaths, nil, pages)
}

// WritePositionedFontImagePDF writes Scribd artwork with its matching embedded
// TrueType fonts. It is used when the document's WOFF2 font subsets were
// available, otherwise WritePositionedSearchableImagePDF provides the fallback.
func WritePositionedFontImagePDF(path string, imagePaths []string, pages []Page, fonts map[string][]byte) error {
	if len(imagePaths) == 0 || len(imagePaths) != len(pages) {
		return fmt.Errorf("images and pages must be non-empty and aligned")
	}
	pdf := gofpdf.New("P", "pt", "Letter", "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	for family, data := range fonts {
		pdf.AddUTF8FontFromBytes(family, "", data)
	}
	for index, imagePath := range imagePaths {
		config, err := imageDimensions(imagePath)
		if err != nil {
			return err
		}
		pageWidth := 612.0
		pageHeight := pageWidth * float64(config.Height) / float64(config.Width)
		pdf.AddPageFormat("P", gofpdf.SizeType{Wd: pageWidth, Ht: pageHeight})
		pdf.ImageOptions(imagePath, 0, 0, pageWidth, pageHeight, false, gofpdf.ImageOptions{ImageType: imageType(imagePath)}, 0, "")
		page := pages[index]
		if page.Width <= 0 {
			page.Width = 901
		}
		scale := pageWidth / float64(page.Width)
		dashTarget := dashAlignmentTarget(pdf, page, scale, fonts)
		baselines := alignedBaselines(page.TextItems)
		for itemIndex, item := range page.TextItems {
			if strings.TrimSpace(item.Text) == "" {
				continue
			}
			size := item.FontSize * .2 * scale
			if size < 3 {
				size = 3
			}
			if size > 100 {
				size = 100
			}
			family := item.Family
			if _, ok := fonts[family]; !ok {
				family = "Helvetica"
			}
			pdf.SetFont(family, "", size)
			pdf.SetTextColor(int(item.Red*255), int(item.Green*255), int(item.Blue*255))
			pdf.SetAlpha(itemOpacity(item), "Normal")
			x := item.Left * .2 * scale
			y := baselines[itemIndex] * .2 * scale
			drawTextItem(pdf, x, y, item, scale, dashTarget)
		}
		pdf.SetAlpha(1, "Normal")
	}
	if err := pdf.Error(); err != nil {
		return err
	}
	return pdf.OutputFileAndClose(path)
}

func itemOpacity(item TextItem) float64 {
	// The zero value keeps manually created TextItems fully opaque while
	// parsed Scribd elements preserve their explicit CSS opacity.
	if item.Opacity <= 0 || item.Opacity > 1 {
		return 1
	}
	return item.Opacity
}

func imageDimensions(path string) (image.Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return image.Config{}, err
	}
	defer file.Close()
	config, _, err := image.DecodeConfig(file)
	return config, err
}

// alignedBaselines preserves Scribd's shared baseline when a line is split
// across fonts with different sizes (for example, “Rnd 12:” and its formula).
// Their top coordinates are intentionally only a few CSS pixels apart; adding
// each font's baseline independently creates a visible vertical drift.
func alignedBaselines(items []TextItem) []float64 {
	baselines := make([]float64, len(items))
	for i, item := range items {
		baselines[i] = item.Top + item.FontSize*.7
	}
	for i := range items {
		for j := range items {
			if i == j || math.Abs(items[i].Top-items[j].Top) > 10 || math.Abs(items[i].FontSize-items[j].FontSize) < 1 {
				continue
			}
			if baselines[j] < baselines[i] {
				baselines[i] = baselines[j]
			}
		}
	}
	return baselines
}

func drawGlyphs(pdf *gofpdf.Fpdf, x, y float64, glyphs []Glyph, scale, letterSpacing, wordSpacing float64) {
	for index, glyph := range glyphs {
		if index > 0 {
			x += letterSpacing * .2 * scale
		}
		x += glyph.Before * .2 * scale
		pdf.Text(x, y, glyph.Text)
		x += pdf.GetStringWidth(glyph.Text)
		if glyph.Text == " " {
			x += wordSpacing * .2 * scale
		}
	}
}

// drawTextItem replays CSS spacing for both nested Scribd glyph spans and
// ordinary text spans. Scribd uses word-spacing even when a line has no
// nested spans, so passing the whole line to FPDF would silently lose it.
func drawTextItem(pdf *gofpdf.Fpdf, x, y float64, item TextItem, scale, dashTarget float64) {
	if len(item.Glyphs) > 0 {
		drawGlyphs(pdf, x, y, item.Glyphs, scale, item.LetterSpacing, item.WordSpacing)
		return
	}
	if item.LetterSpacing == 0 && item.WordSpacing == 0 {
		drawSpacedText(pdf, x, y, item.Text, dashTarget)
		return
	}
	if item.LetterSpacing != 0 {
		glyphs := make([]Glyph, 0, len(item.Text))
		for _, r := range item.Text {
			glyphs = append(glyphs, Glyph{Text: string(r)})
		}
		drawGlyphs(pdf, x, y, glyphs, scale, item.LetterSpacing, item.WordSpacing)
		return
	}
	// Keep each non-space word as a normal text run so font kerning remains
	// intact, and adjust only the CSS-defined gap following each space.
	for _, run := range strings.SplitAfter(item.Text, " ") {
		pdf.Text(x, y, run)
		x += pdf.GetStringWidth(run)
		if strings.HasSuffix(run, " ") {
			x += item.WordSpacing * .2 * scale
		}
	}
}

func dashAlignmentTarget(pdf *gofpdf.Fpdf, page Page, scale float64, fonts map[string][]byte) float64 {
	target := -1.0
	for _, item := range page.TextItems {
		if item.LetterSpacing == 0 || !strings.Contains(item.Text, "-") || encodedGlyphRun(item.Text) {
			continue
		}
		size := item.FontSize * .2 * scale
		if size < 3 {
			size = 3
		}
		if size > 100 {
			size = 100
		}
		family := item.Family
		if _, ok := fonts[family]; !ok {
			family = "Helvetica"
		}
		pdf.SetFont(family, "", size)
		prefix := item.Text[:strings.Index(item.Text, "-")]
		natural := item.Left*.2*scale + pdf.GetStringWidth(prefix)
		if natural > target {
			target = natural
		}
	}
	if target >= 0 {
		// Reserve one normal space before every aligned separator.
		return target + pdf.GetStringWidth(" ")
	}
	return target
}

// encodedGlyphRun identifies Scribd's subset-font character streams. Some
// publishers map printable punctuation (including '-') to unrelated glyphs.
// Treating those bytes as visible separators corrupts the layout, while normal
// prose and crochet instructions remain eligible for dash alignment.
func encodedGlyphRun(text string) bool {
	visible, letters := 0, 0
	for _, r := range text {
		if unicode.IsSpace(r) {
			continue
		}
		visible++
		if unicode.IsLetter(r) {
			letters++
		}
	}
	return visible > 0 && letters*3 < visible
}

func drawSpacedText(pdf *gofpdf.Fpdf, x, y float64, text string, dashTarget float64) {
	if dashTarget < 0 || !strings.Contains(text, "-") || encodedGlyphRun(text) {
		pdf.Text(x, y, text)
		return
	}
	dash := strings.IndexByte(text, '-')
	prefix, suffix := text[:dash], text[dash:]
	pdf.Text(x, y, prefix)
	dashX := dashTarget
	if natural := x + pdf.GetStringWidth(prefix); dashX < natural {
		dashX = natural
	}
	// Keep a readable separator: the aligned dash is surrounded by one
	// ordinary font space, matching the document's intended “text - text”
	// layout even when the source span omitted literal whitespace.
	pdf.Text(dashX, y, "-")
	if len(suffix) > 1 {
		pdf.Text(dashX+pdf.GetStringWidth("- "), y, suffix[1:])
	}
	return
}

func imageType(path string) string {
	if strings.EqualFold(filepath.Ext(path), ".png") {
		return "PNG"
	}
	return "JPG"
}

func writeImagePDF(path string, imagePaths, texts []string, positionedPages []Page) error {
	if len(imagePaths) == 0 {
		return fmt.Errorf("no images to write")
	}
	type item struct {
		data          []byte
		width, height int
	}
	items := make([]item, 0, len(imagePaths))
	for _, path := range imagePaths {
		raw, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		config, format, e := image.DecodeConfig(bytes.NewReader(raw))
		if e != nil {
			return e
		}
		// Preserve Scribd's JPEG bytes exactly.  Re-encoding made the Go
		// version visibly softer than the reference Python PDF.
		if format == "jpeg" {
			items = append(items, item{raw, config.Width, config.Height})
			continue
		}
		img, _, e := image.Decode(bytes.NewReader(raw))
		if e != nil {
			return e
		}
		var b bytes.Buffer
		if e = jpeg.Encode(&b, img, &jpeg.Options{Quality: 95}); e != nil {
			return e
		}
		bounds := img.Bounds()
		items = append(items, item{b.Bytes(), bounds.Dx(), bounds.Dy()})
	}
	searchable := texts != nil || positionedPages != nil
	// Object 2 must always be the Pages tree because Catalog references 2 0 R.
	// Reserve it before adding the optional font object.
	objects := [][]byte{[]byte("<< /Type /Catalog /Pages 2 0 R >>"), nil}
	kids := make([]string, len(items))
	firstPage := 3
	if searchable {
		objects = append(objects, []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"))
		firstPage = 4
	}
	for i := range items {
		kids[i] = fmt.Sprintf("%d 0 R", firstPage+i*3)
	}
	objects[1] = []byte("<< /Type /Pages /Kids [ " + strings.Join(kids, " ") + " ] /Count " + strconv.Itoa(len(items)) + " >>")
	for i, it := range items {
		pageNum := firstPage + i*3
		imageNum := pageNum + 1
		contentNum := pageNum + 2
		w := 612.0
		h := w * float64(it.height) / float64(it.width)
		resources := fmt.Sprintf("/XObject << /Im0 %d 0 R >>", imageNum)
		if searchable {
			resources += " /Font << /F1 3 0 R >>"
		}
		objects = append(objects, []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.2f %.2f] /Resources << %s >> /Contents %d 0 R >>", w, h, resources, contentNum)))
		objects = append(objects, append([]byte(fmt.Sprintf("<< /Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length %d >>\nstream\n", it.width, it.height, len(it.data))), append(it.data, []byte("\nendstream")...)...))
		content := []byte(fmt.Sprintf("q\n%.2f 0 0 %.2f 0 0 cm\n/Im0 Do\nQ\n", w, h))
		if positionedPages != nil && i < len(positionedPages) {
			content = append(content, positionedText(positionedPages[i], w, h)...)
		} else if searchable && i < len(texts) && strings.TrimSpace(texts[i]) != "" {
			content = append(content, []byte("BT\n/F1 1 Tf\n0 0 0 rg\n1 1 Td\n("+escapePDF(pdfASCII(texts[i]))+") Tj\nET\n")...)
		}
		objects = append(objects, append([]byte(fmt.Sprintf("<< /Length %d >>\nstream\n", len(content))), append(content, []byte("endstream")...)...))
	}
	return writePDF(path, objects)
}

func positionedText(page Page, pageWidth, pageHeight float64) []byte {
	if page.Width <= 0 {
		page.Width = 901
	}
	if page.Height <= 0 {
		page.Height = 1275
	}
	scale := pageWidth / float64(page.Width)
	var content strings.Builder
	for _, item := range page.TextItems {
		if strings.TrimSpace(item.Text) == "" {
			continue
		}
		size := item.FontSize * .2 * scale
		if size < 3 {
			size = 3
		}
		if size > 100 {
			size = 100
		}
		x := item.Left * .2 * scale
		y := pageHeight - item.Top*.2*scale - (item.FontSize * .2 * scale * .7)
		fmt.Fprintf(&content, "BT\n/F1 %.2f Tf\n%.3f %.3f %.3f rg\n%.2f %.2f Td\n(%s) Tj\nET\n", size, item.Red, item.Green, item.Blue, x, y, escapePDF(pdfASCII(item.Text)))
	}
	if content.Len() == 0 && strings.TrimSpace(page.Text) != "" {
		content.WriteString("BT\n/F1 1 Tf\n0 0 0 rg\n1 1 Td\n(" + escapePDF(pdfASCII(page.Text)) + ") Tj\nET\n")
	}
	return []byte(content.String())
}

func pdfASCII(value string) string {
	return strings.Map(func(r rune) rune {
		if r < 32 || r > 126 {
			return ' '
		}
		return r
	}, value)
}

func escapePDF(s string) string {
	return strings.NewReplacer("\\", "\\\\", "(", "\\(", ")", "\\)", "\r", " ", "\n", " ").Replace(s)
}
func WriteTextPDF(path, title string, texts []string) error {
	objects := [][]byte{[]byte("<< /Type /Catalog /Pages 2 0 R >>")}
	kids := make([]string, len(texts))
	for i := range texts {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i*2)
	}
	objects = append(objects, []byte("<< /Type /Pages /Kids [ "+strings.Join(kids, " ")+" ] /Count "+strconv.Itoa(len(texts))+" >>"), []byte("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"))
	for i, text := range texts {
		pageNum := 4 + i*2
		contentNum := pageNum + 1
		objects = append(objects, []byte(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentNum)))
		lines := []string{title}
		for _, word := range strings.Fields(text) {
			if len(lines) == 1 || len(lines[len(lines)-1])+len(word) > 95 {
				lines = append(lines, word)
			} else {
				lines[len(lines)-1] += " " + word
			}
		}
		var content strings.Builder
		content.WriteString("BT /F1 12 Tf 40 800 Td ")
		for n, line := range lines {
			if n > 0 {
				content.WriteString(" 0 -16 Td ")
			}
			content.WriteString("(" + escapePDF(line) + ") Tj")
		}
		content.WriteString(" ET")
		data := []byte(content.String())
		objects = append(objects, append([]byte(fmt.Sprintf("<< /Length %d >>\nstream\n", len(data))), append(data, []byte("\nendstream")...)...))
	}
	return writePDF(path, objects)
}
func writePDF(path string, objects [][]byte) error {
	part := path + ".part"
	file, err := os.OpenFile(part, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	removePart := true
	defer func() {
		_ = file.Close()
		if removePart {
			_ = os.Remove(part)
		}
	}()
	written := 0
	write := func(data []byte) error {
		n, err := file.Write(data)
		written += n
		return err
	}
	writeString := func(value string) error { return write([]byte(value)) }
	if err := writeString("%PDF-1.4\n%"); err != nil {
		return err
	}
	if err := write([]byte{0xe2, 0xe3, 0xcf, 0xd3, '\n'}); err != nil {
		return err
	}
	offsets := make([]int, len(objects))
	for i, obj := range objects {
		offsets[i] = written
		if err := writeString(fmt.Sprintf("%d 0 obj\n", i+1)); err != nil {
			return err
		}
		if err := write(obj); err != nil {
			return err
		}
		if err := writeString("\nendobj\n"); err != nil {
			return err
		}
	}
	xref := written
	if err := writeString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)); err != nil {
		return err
	}
	for _, offset := range offsets {
		if err := writeString(fmt.Sprintf("%010d 00000 n \n", offset)); err != nil {
			return err
		}
	}
	if err := writeString(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(part, path); err != nil {
		return err
	}
	removePart = false
	return nil
}

func SortPages(pages []Page) {
	sort.Slice(pages, func(i, j int) bool { return pages[i].Number < pages[j].Number })
}
