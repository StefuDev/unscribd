package unscribd

import "regexp"

var (
	docIDRE       = regexp.MustCompile(`scribd\.com/(?:document|doc)/(\d+)`)
	assetRE       = regexp.MustCompile(`docManager\.assetPrefix\s*=\s*"([^"]+)"`)
	contentRE     = regexp.MustCompile(`contentUrl:\s*"([^"]+)"`)
	displayRE     = regexp.MustCompile(`docManager\.displayType\s*=\s*"([^"]+)"`)
	pageCountRE   = regexp.MustCompile(`"page_count"\s*:\s*(\d+)`)
	pageSizeRE    = regexp.MustCompile(`width:\s*(\d+)px;\s*height:\s*(\d+)px`)
	fontDivRE     = regexp.MustCompile(`(?is)<div([^>]*font-size[^>]*)>(.*?)</div>`)
	spanAttrRE    = regexp.MustCompile(`(?is)<span([^>]*)>(.*?)</span>`)
	spanTagRE     = regexp.MustCompile(`(?is)</?span\b[^>]*>`)
	attrRE        = regexp.MustCompile(`(?is)([a-zA-Z-]+)="([^"]*)"`)
	shortTitleRE  = regexp.MustCompile(`"short_title"\s*:\s*"([^"]+)"`)
	titleRE       = regexp.MustCompile(`"title"\s*:\s*"([^"]+)"`)
	htmlTitleRE   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	imageRE       = regexp.MustCompile(`(?is)<img[^>]+(?:orig|src)="([^"]+)"`)
	spanRE        = regexp.MustCompile(`(?is)<span[^>]*>(.*?)</span>`)
	tagRE         = regexp.MustCompile(`(?is)<[^>]+>`)
	jsonpRE       = regexp.MustCompile(`(?s)^\s*window\.page\d+_callback\(\[(.*)\]\);?\s*$`)
	invalidFileRE = regexp.MustCompile(`[\\/*?:"<>|]`)
)

const userAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
