package unscribd

type PageInfo struct {
	Number     int
	ContentURL string
}

type Document struct {
	ID          string
	URL         string
	Title       string
	AssetPrefix string
	DisplayType string
	PageCount   int
	Pages       []PageInfo
}

type Page struct {
	Number    int
	HTML      string
	Text      string
	Images    []string
	Width     int
	Height    int
	TextItems []TextItem
}

type TextItem struct {
	Text          string
	Left          float64
	Top           float64
	FontSize      float64
	Red           float64
	Green         float64
	Blue          float64
	Family        string
	LetterSpacing float64
	WordSpacing   float64
	Opacity       float64
	Glyphs        []Glyph
}

type Glyph struct {
	Text   string
	Before float64
}

type Options struct {
	Format      string
	Output      string
	ImagesDir   string
	Token       string
	CacheDir    string
	CacheBytes  int64
	NoToken     bool
	Concurrency int
}

type Result struct {
	Title  string
	Output string
	Pages  int
}

type documentManifest struct {
	Title     string
	PageCount int
}
