package unscribd

import (
	"html"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

func extractPage(number int, markup string) Page {
	seen := map[string]bool{}
	images := []string{}
	for _, match := range imageRE.FindAllStringSubmatch(markup, -1) {
		u := html.UnescapeString(match[1])
		u = strings.Replace(u, "http://html.scribd.com", "https://html.scribdassets.com", 1)
		if strings.Contains(u, "html.scribd") && !seen[u] {
			seen[u] = true
			images = append(images, u)
		}
	}
	text := []string{}
	for _, match := range spanRE.FindAllStringSubmatch(markup, -1) {
		v := strings.TrimSpace(html.UnescapeString(tagRE.ReplaceAllString(match[1], "")))
		if v != "" {
			text = append(text, v)
		}
	}
	width, height := 901, 1275
	if size := pageSizeRE.FindStringSubmatch(markup); len(size) == 3 {
		width, _ = strconv.Atoi(size[1])
		height, _ = strconv.Atoi(size[2])
	}
	return Page{Number: number, HTML: markup, Text: strings.Join(text, " "), Images: images, Width: width, Height: height, TextItems: extractTextItems(markup)}
}

func attributes(tag string) map[string]string {
	result := map[string]string{}
	for _, m := range attrRE.FindAllStringSubmatch(tag, -1) {
		result[strings.ToLower(m[1])] = html.UnescapeString(m[2])
	}
	return result
}
func styles(value string) map[string]string {
	result := map[string]string{}
	for _, part := range strings.Split(value, ";") {
		pair := strings.SplitN(part, ":", 2)
		if len(pair) == 2 {
			result[strings.TrimSpace(pair[0])] = strings.TrimSpace(pair[1])
		}
	}
	return result
}
func pixels(value string) (float64, bool) {
	number, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(value, "px")), 64)
	return number, err == nil
}
func opacity(value string) float64 {
	if value == "" {
		return 1
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil || parsed < 0 || parsed > 1 {
		return 1
	}
	return parsed
}
func visibleText(markup string) string {
	// Regular indentation around spans is not content, while &nbsp; is a real
	// layout character used to offset adjacent positioned runs. Trim only ASCII
	// HTML whitespace so non-breaking spaces survive into the PDF renderer.
	return strings.Trim(html.UnescapeString(tagRE.ReplaceAllString(markup, "")), " \t\r\n")
}
func extractTextItems(markup string) []TextItem {
	items := []TextItem{}
	for _, div := range fontDivRE.FindAllStringSubmatch(markup, -1) {
		divAttributes := attributes(div[1])
		fontSize, ok := pixels(styles(divAttributes["style"])["font-size"])
		if !ok {
			continue
		}
		for _, span := range positionedTextSpans(div[2]) {
			style := span.style
			left, lok := pixels(style["left"])
			top, tok := pixels(style["top"])
			text := visibleText(span.body)
			if lok && tok && text != "" {
				letterSpacing, _ := pixels(style["letter-spacing"])
				wordSpacing, _ := pixels(style["word-spacing"])
				opacityValue := style["opacity"]
				if opacityValue == "" {
					opacityValue = styles(divAttributes["style"])["opacity"]
				}
				color := style["color"]
				if color == "" {
					color = styles(divAttributes["style"])["color"]
				}
				r, g, b, keep := pdfColor(color)
				keep = keep || keepLightText(r, g, b)
				if keep {
					family := ""
					for _, class := range strings.Fields(divAttributes["class"]) {
						if strings.HasPrefix(class, "ff") {
							family = class
							break
						}
					}
					items = append(items, TextItem{Text: text, Left: left, Top: top, FontSize: fontSize, Red: r, Green: g, Blue: b, Family: family, LetterSpacing: letterSpacing, WordSpacing: wordSpacing, Opacity: opacity(opacityValue), Glyphs: parseGlyphs(span.body)})
				}
			}
		}
	}
	if len(items) > 0 {
		return items
	}
	for _, span := range positionedTextSpans(markup) {
		style := span.style
		left, lok := pixels(style["left"])
		top, tok := pixels(style["top"])
		size, _ := pixels(style["font-size"])
		text := visibleText(span.body)
		if lok && tok && text != "" {
			if size == 0 {
				size = 12
			}
			r, g, b, keep := pdfColor(style["color"])
			keep = keep || keepLightText(r, g, b)
			if keep {
				letterSpacing, _ := pixels(style["letter-spacing"])
				wordSpacing, _ := pixels(style["word-spacing"])
				items = append(items, TextItem{Text: text, Left: left, Top: top, FontSize: size, Red: r, Green: g, Blue: b, LetterSpacing: letterSpacing, WordSpacing: wordSpacing, Opacity: opacity(style["opacity"]), Glyphs: parseGlyphs(span.body)})
			}
		}
	}
	return items
}

func parseGlyphs(fragment string) []Glyph {
	nodes, err := xhtml.ParseFragment(strings.NewReader(fragment), nil)
	if err != nil {
		return nil
	}
	glyphs := []Glyph{}
	pending := 0.0
	hasElement := false
	var walk func(*xhtml.Node)
	walk = func(node *xhtml.Node) {
		if node.Type == xhtml.TextNode {
			for _, r := range []rune(html.UnescapeString(node.Data)) {
				glyphs = append(glyphs, Glyph{Text: string(r), Before: pending})
				pending = 0
			}
			return
		}
		if node.Type != xhtml.ElementNode {
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				walk(child)
			}
			return
		}
		// ParseFragment wraps plain content in html/body nodes. Only actual
		// spans represent Scribd's per-glyph layout and need replaying.
		if strings.EqualFold(node.Data, "span") {
			hasElement = true
		}
		attrs := map[string]string{}
		for _, attr := range node.Attr {
			attrs[strings.ToLower(attr.Key)] = attr.Val
		}
		style := styles(attrs["style"])
		if margin, ok := pixels(style["margin-left"]); ok {
			pending += margin
		}
		isWidthSpacer := false
		for _, class := range strings.Fields(attrs["class"]) {
			// Scribd commonly emits compact CSS classes instead of inline
			// styles: l10 means margin-left:-10px and w6 means width:6px.
			// They cancel the outer letter spacing character by character.
			if len(class) > 1 && class[0] == 'l' {
				if amount, err := strconv.ParseFloat(class[1:], 64); err == nil {
					pending -= amount
				}
			}
			if class == "w" || (len(class) > 1 && class[0] == 'w') {
				isWidthSpacer = true
				if style["width"] == "" && len(class) > 1 {
					if amount, err := strconv.ParseFloat(class[1:], 64); err == nil {
						pending += amount
					}
				}
			}
		}
		if isWidthSpacer {
			if width, ok := pixels(style["width"]); ok {
				pending += width
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	for _, node := range nodes {
		walk(node)
	}
	if !hasElement {
		return nil
	}
	return glyphs
}

type textSpan struct {
	body  string
	style map[string]string
}

// positionedTextSpans returns Scribd's positioned text spans (class="a").
// Some documents put the vertical coordinate on a parent class="g" span and
// the horizontal coordinate on its child. Replaying only the child silently
// drops the parent coordinate and, in older code, dropped that text entirely.
// The HTML tree also keeps nested l/w glyph spans intact for parseGlyphs.
func positionedTextSpans(markup string) []textSpan {
	nodes, err := xhtml.ParseFragment(strings.NewReader(markup), nil)
	if err != nil {
		return nil
	}
	result := []textSpan{}
	var walk func(*xhtml.Node, map[string]string)
	walk = func(node *xhtml.Node, inherited map[string]string) {
		current := inherited
		if node.Type == xhtml.ElementNode && strings.EqualFold(node.Data, "span") {
			own := map[string]string{}
			class := ""
			for _, attr := range node.Attr {
				switch strings.ToLower(attr.Key) {
				case "style":
					own = styles(attr.Val)
				case "class":
					class = attr.Val
				}
			}
			current = mergeStyles(inherited, own)
			// Most positioned runs use class="a". Older exports omit the class
			// entirely, but still provide left/top on the outer span.
			if hasClass(class, "a") || class == "" {
				var body strings.Builder
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					_ = xhtml.Render(&body, child)
				}
				result = append(result, textSpan{body: body.String(), style: current})
				return
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, current)
		}
	}
	for _, node := range nodes {
		walk(node, nil)
	}
	return result
}

func mergeStyles(base, own map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(own))
	for key, value := range base {
		merged[key] = value
	}
	for key, value := range own {
		merged[key] = value
	}
	return merged
}

func hasClass(classes, target string) bool {
	for _, class := range strings.Fields(classes) {
		if class == target {
			return true
		}
	}
	return false
}

// pdfColor converts Scribd's text-layer color to PDF RGB. Near-white colors
// are flagged so callers can distinguish them from ordinary text colors.
func pdfColor(value string) (float64, float64, float64, bool) {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
	if value == "" {
		return 0, 0, 0, true
	}
	if strings.HasPrefix(value, "#") && len(value) == 7 {
		r, er := strconv.ParseInt(value[1:3], 16, 64)
		g, eg := strconv.ParseInt(value[3:5], 16, 64)
		b, eb := strconv.ParseInt(value[5:7], 16, 64)
		if er == nil && eg == nil && eb == nil {
			if r > 250 && g > 250 && b > 250 && !(r == 255 && g == 255 && b == 255) {
				return float64(r) / 255, float64(g) / 255, float64(b) / 255, false
			}
			return float64(r) / 255, float64(g) / 255, float64(b) / 255, true
		}
	}
	return 0, 0, 0, true
}

func keepLightText(r, g, b float64) bool {
	return r > 0.98 && g > 0.98 && b > 0.98
}
