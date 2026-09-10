package unscribd

import (
	"fmt"
	"html"
	"os"
	"strconv"
	"strings"

	"github.com/phpdave11/gofpdf"
)

type imageTile struct {
	path                 string
	left, top            float64
	clipTop, clipRight   float64
	clipBottom, clipLeft float64
	width, height        int
}

func directImageTiles(page Page, sources map[string]string) ([]imageTile, error) {
	tags := absImgTagRE.FindAllString(page.HTML, -1)
	if len(tags) == 0 {
		return nil, fmt.Errorf("page %d has no positioned image tiles", page.Number)
	}
	tiles := make([]imageTile, 0, len(tags))
	for _, tag := range tags {
		clip := clipRE.FindStringSubmatch(tag)
		urlMatch := urlRE.FindStringSubmatch(tag)
		if len(clip) != 5 || len(urlMatch) != 2 {
			return nil, fmt.Errorf("page %d has an unsupported image tile", page.Number)
		}
		rawURL := html.UnescapeString(urlMatch[1])
		path := sources[rawURL]
		if path == "" {
			path = sources[strings.Replace(rawURL, "http://html.scribd.com", "https://html.scribdassets.com", 1)]
		}
		if path == "" {
			return nil, fmt.Errorf("page %d image tile is unavailable", page.Number)
		}
		config, err := imageDimensions(path)
		if err != nil {
			return nil, err
		}
		attrs := attributes(strings.TrimSuffix(strings.TrimPrefix(tag, "<img"), ">"))
		css := styles(attrs["style"])
		left, _ := pixels(css["left"])
		top, _ := pixels(css["top"])
		values := make([]float64, 4)
		for i := range values {
			values[i], err = strconv.ParseFloat(clip[i+1], 64)
			if err != nil {
				return nil, err
			}
		}
		if values[1] <= values[3] || values[2] <= values[0] {
			return nil, fmt.Errorf("page %d has an empty image tile", page.Number)
		}
		tiles = append(tiles, imageTile{path: path, left: left, top: top, clipTop: values[0], clipRight: values[1], clipBottom: values[2], clipLeft: values[3], width: config.Width, height: config.Height})
	}
	return tiles, nil
}

// WriteDirectPositionedFontPDF reproduces Scribd's CSS clipping directly with
// PDF clipping paths. Original JPEG/PNG assets are embedded without creating
// intermediate page-sized PNG files.
func WriteDirectPositionedFontPDF(path string, pages []Page, sources []map[string]string, fallbacks []string, fonts map[string][]byte) error {
	if len(pages) == 0 || len(pages) != len(sources) || len(pages) != len(fallbacks) {
		return fmt.Errorf("pages, sources, and fallbacks must be non-empty and aligned")
	}
	pdf := gofpdf.New("P", "pt", "Letter", "")
	pdf.SetMargins(0, 0, 0)
	pdf.SetAutoPageBreak(false, 0)
	for family, data := range fonts {
		pdf.AddUTF8FontFromBytes(family, "", data)
	}
	for index, page := range pages {
		if page.Width <= 0 || page.Height <= 0 {
			return fmt.Errorf("page %d has invalid dimensions", page.Number)
		}
		pageWidth := 612.0
		pageHeight := pageWidth * float64(page.Height) / float64(page.Width)
		pdf.AddPageFormat("P", gofpdf.SizeType{Wd: pageWidth, Ht: pageHeight})
		scale := pageWidth / float64(page.Width)
		if fallbacks[index] != "" {
			pdf.ImageOptions(fallbacks[index], 0, 0, pageWidth, pageHeight, false, gofpdf.ImageOptions{ImageType: imageType(fallbacks[index])}, 0, "")
		} else {
			tiles, err := directImageTiles(page, sources[index])
			if err != nil {
				return err
			}
			for _, tile := range tiles {
				x := (tile.left + tile.clipLeft) * scale
				y := (tile.top + tile.clipTop) * scale
				width := (tile.clipRight - tile.clipLeft) * scale
				height := (tile.clipBottom - tile.clipTop) * scale
				pdf.ClipRect(x, y, width, height, false)
				pdf.ImageOptions(tile.path, tile.left*scale, tile.top*scale, float64(tile.width)*scale, float64(tile.height)*scale, false, gofpdf.ImageOptions{ImageType: imageType(tile.path)}, 0, "")
				pdf.ClipEnd()
			}
		}
		drawPositionedFontText(pdf, page, pageWidth, fonts)
	}
	if err := pdf.Error(); err != nil {
		return err
	}
	part := path + ".part"
	if err := pdf.OutputFileAndClose(part); err != nil {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, path)
}

func needsRasterFallback(page Page, sources map[string]string) bool {
	tiles, err := directImageTiles(page, sources)
	return err != nil || len(tiles) != 1
}

func drawPositionedFontText(pdf *gofpdf.Fpdf, page Page, pageWidth float64, fonts map[string][]byte) {
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
