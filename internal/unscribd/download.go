package unscribd

import (
	"context"
	"fmt"
	"html"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

func fetchPage(ctx context.Context, client *http.Client, page PageInfo, token string) (Page, error) {
	body, status, err := request(ctx, client, http.MethodGet, appendToken(page.ContentURL, token), nil, map[string]string{"Referer": "https://www.scribd.com/"})
	if err != nil {
		return Page{}, err
	}
	if status != http.StatusOK {
		return Page{}, fmt.Errorf("page %d returned %d", page.Number, status)
	}
	markup, err := ParseJSONP(string(body))
	if err != nil {
		return Page{}, err
	}
	return extractPage(page.Number, markup), nil
}

func download(ctx context.Context, client *http.Client, raw, path, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, appendToken(raw, token), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	req.Header.Set("Referer", "https://www.scribd.com/")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("image returned %d", resp.StatusCode)
	}
	file, err := os.OpenFile(path+".part", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(path + ".part")
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(path + ".part")
		return closeErr
	}
	return os.Rename(path+".part", path)
}

func Download(ctx context.Context, source string, options Options) (Result, error) {
	profile := newProfiler()
	defer profile.total()
	options.Concurrency = effectiveConcurrency(options.Concurrency)
	if options.Format == "" {
		options.Format = "pdf"
	}
	id, err := ExtractDocumentID(source)
	if err != nil {
		return Result{}, err
	}
	cacheDir := cacheDirectory(options.CacheDir)
	cacheBytes := cacheLimit(options.CacheBytes)
	if options.Format == "pdf" {
		var manifest documentManifest
		if cacheReadJSON(cacheDir, "manifest|"+id, &manifest) && manifest.Title != "" {
			cachedOutput := options.Output
			if cachedOutput == "" {
				cachedOutput = SanitizeFilename(manifest.Title) + ".pdf"
			}
			if cacheGet(cacheDir, "pdf|"+id+"|"+manifest.Title, cachedOutput) {
				return Result{Title: manifest.Title, Output: cachedOutput, Pages: manifest.PageCount}, nil
			}
		}
	}
	client, err := NewClient()
	if err != nil {
		return Result{}, err
	}
	stage := time.Now()
	doc, err := FetchDocument(ctx, client, id)
	if err != nil {
		return Result{}, err
	}
	profile.stage("document fetch", stage)
	cacheWriteJSON(cacheDir, "manifest|"+id, documentManifest{Title: doc.Title, PageCount: doc.PageCount})
	output := options.Output
	if output == "" {
		output = SanitizeFilename(doc.Title)
		if options.Format != "jsonp" && options.Format != "images" {
			output += ".pdf"
		}
	}
	pdfCacheKey := "pdf|" + id + "|" + doc.Title
	if options.Format == "pdf" {
		if cacheGet(cacheDir, pdfCacheKey, output) {
			return Result{Title: doc.Title, Output: output, Pages: doc.PageCount}, nil
		}
	}
	token := options.Token
	if token == "" && !options.NoToken {
		stage = time.Now()
		token = FetchToken(ctx, client, id, "")
		profile.stage("token fetch", stage)
	}
	stage = time.Now()
	pageCacheKey := "pages|" + id + "|" + doc.AssetPrefix
	var pages []Page
	if cacheReadJSON(cacheDir, pageCacheKey, &pages) && len(pages) == len(doc.Pages) {
		profile.stage("page cache", stage)
	} else {
		pages = make([]Page, len(doc.Pages))
		jobs := make(chan int)
		var wg sync.WaitGroup
		var firstErr error
		var errMu sync.Mutex
		for n := 0; n < options.Concurrency; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for index := range jobs {
					page, e := fetchPage(ctx, client, doc.Pages[index], token)
					if e != nil {
						errMu.Lock()
						if firstErr == nil {
							firstErr = e
						}
						errMu.Unlock()
						continue
					}
					pages[index] = page
				}
			}()
		}
		for i := range doc.Pages {
			jobs <- i
		}
		close(jobs)
		wg.Wait()
		if firstErr != nil {
			return Result{}, firstErr
		}
		cacheWriteJSON(cacheDir, pageCacheKey, pages)
		profile.stage("page fetch", stage)
	}
	if options.Format == "jsonp" {
		if err := os.MkdirAll(output, 0755); err != nil {
			return Result{}, err
		}
		for _, page := range pages {
			if err := os.WriteFile(filepath.Join(output, fmt.Sprintf("page-%03d.html", page.Number)), []byte(page.HTML), 0600); err != nil {
				return Result{}, err
			}
		}
		return Result{Title: doc.Title, Output: output, Pages: len(pages)}, nil
	}
	if options.Format == "text" {
		if !strings.HasSuffix(strings.ToLower(output), ".pdf") {
			output += ".pdf"
		}
		texts := make([]string, len(pages))
		for i, p := range pages {
			texts[i] = p.Text
		}
		return Result{Title: doc.Title, Output: output, Pages: len(pages)}, WriteTextPDF(output, doc.Title, texts)
	}
	imagesDir := options.ImagesDir
	if imagesDir == "" {
		imagesDir = strings.TrimSuffix(output, ".pdf") + "_images"
	}
	if options.Format == "images" && options.Output != "" {
		imagesDir = options.Output
	}
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return Result{}, err
	}
	paths := make([]string, len(pages))
	sourcePaths := make([]map[string]string, len(pages))
	type assetTask struct{ raw, path string }
	assets := make([]assetTask, 0)
	knownAssets := make(map[string]string)
	for i, p := range pages {
		if len(p.Images) == 0 {
			continue
		}
		ext := filepath.Ext(strings.Split(p.Images[0], "?")[0])
		if ext == "" {
			ext = ".png"
		}
		paths[i] = filepath.Join(imagesDir, fmt.Sprintf("page-%03d%s", p.Number, ext))
		sourcePaths[i] = map[string]string{}
		for imageIndex, imageURL := range p.Images {
			imageExt := filepath.Ext(strings.Split(imageURL, "?")[0])
			if imageExt == "" {
				imageExt = ".png"
			}
			imagePath := paths[i]
			if imageIndex > 0 {
				imagePath = filepath.Join(imagesDir, fmt.Sprintf("page-%03d-source-%02d%s", p.Number, imageIndex+1, imageExt))
			}
			if existing := knownAssets[imageURL]; existing != "" {
				imagePath = existing
				if imageIndex == 0 {
					paths[i] = imagePath
				}
			} else {
				knownAssets[imageURL] = imagePath
				if _, err := os.Stat(imagePath); err != nil {
					assets = append(assets, assetTask{raw: imageURL, path: imagePath})
				}
			}
			sourcePaths[i][imageURL] = imagePath
		}
	}
	stage = time.Now()
	assetWorkers := options.Concurrency
	if assetWorkers > 8 {
		assetWorkers = 8
	}
	assetJobs := make(chan assetTask)
	var assetWG sync.WaitGroup
	var assetErr error
	var assetErrOnce sync.Once
	for n := 0; n < assetWorkers; n++ {
		assetWG.Add(1)
		go func() {
			defer assetWG.Done()
			for task := range assetJobs {
				assetKey := "image|" + task.raw + "|" + token
				if cacheGet(cacheDir, assetKey, task.path) {
					continue
				}
				if err := os.MkdirAll(cacheDir, 0755); err == nil {
					cachedPath := cachePath(cacheDir, assetKey)
					if err := download(ctx, client, task.raw, cachedPath, token); err == nil && cacheGet(cacheDir, assetKey, task.path) {
						continue
					}
				}
				if err := download(ctx, client, task.raw, task.path, token); err != nil {
					assetErrOnce.Do(func() { assetErr = err })
					continue
				}
				cachePut(cacheDir, "image|"+task.raw+"|"+token, task.path, cacheBytes)
			}
		}()
	}
	for _, task := range assets {
		assetJobs <- task
	}
	close(assetJobs)
	assetWG.Wait()
	if assetErr != nil {
		return Result{}, assetErr
	}
	trimCache(cacheDir, cacheBytes)
	profile.stage("asset download", stage)
	if options.Format == "pdf" {
		if !strings.HasSuffix(strings.ToLower(output), ".pdf") {
			output += ".pdf"
		}
		directPages := make([]Page, 0, len(pages))
		directSources := make([]map[string]string, 0, len(pages))
		for i, page := range pages {
			if len(sourcePaths[i]) > 0 {
				directPages = append(directPages, page)
				directSources = append(directSources, sourcePaths[i])
			}
		}
		if len(directPages) == len(pages) {
			directCompatible := true
			for i, page := range directPages {
				if needsRasterFallback(page, directSources[i]) {
					directCompatible = false
					break
				}
			}
			if directCompatible {
				stage = time.Now()
				fonts := fetchFonts(ctx, client, doc.AssetPrefix, directPages)
				profile.stage("font fetch", stage)
				stage = time.Now()
				if pdfErr := WriteDirectPositionedFontPDF(output, directPages, directSources, make([]string, len(directPages)), fonts); pdfErr == nil {
					profile.stage("direct PDF write", stage)
					cachePut(cacheDir, pdfCacheKey, output, cacheBytes)
					trimCache(cacheDir, cacheBytes)
					return Result{Title: doc.Title, Output: output, Pages: len(directPages)}, nil
				}
			}
		}
	}
	stage = time.Now()
	composeJobs := make(chan int)
	var composeWG sync.WaitGroup
	composeWorkers := options.Concurrency
	if composeWorkers > 4 {
		composeWorkers = 4
	}
	for n := 0; n < composeWorkers; n++ {
		composeWG.Add(1)
		go func() {
			defer composeWG.Done()
			for i := range composeJobs {
				if composed := composePageImage(pages[i], sourcePaths[i], imagesDir); composed != "" {
					paths[i] = composed
				}
			}
		}()
	}
	for i := range pages {
		if len(sourcePaths[i]) > 0 {
			composeJobs <- i
		}
	}
	close(composeJobs)
	composeWG.Wait()
	profile.stage("image compose", stage)
	if options.Format == "images" {
		return Result{Title: doc.Title, Output: imagesDir, Pages: len(pages)}, nil
	}
	valid, overlayPages := []string{}, []Page{}
	for i, p := range paths {
		if p != "" {
			valid = append(valid, p)
			overlayPages = append(overlayPages, pages[i])
		}
	}
	if len(valid) == 0 {
		texts := make([]string, len(pages))
		for i, p := range pages {
			texts[i] = p.Text
		}
		return Result{Title: doc.Title, Output: output, Pages: len(pages)}, WriteTextPDF(output, doc.Title, texts)
	}
	if !strings.HasSuffix(strings.ToLower(output), ".pdf") {
		output += ".pdf"
	}
	stage = time.Now()
	fonts := fetchFonts(ctx, client, doc.AssetPrefix, overlayPages)
	profile.stage("font fetch", stage)
	stage = time.Now()
	if len(fonts) > 0 {
		pdfErr := WritePositionedFontImagePDF(output, valid, overlayPages, fonts)
		profile.stage("PDF write", stage)
		if pdfErr == nil {
			cachePut(cacheDir, pdfCacheKey, output, cacheBytes)
		}
		return Result{Title: doc.Title, Output: output, Pages: len(valid)}, pdfErr
	}
	pdfErr := WritePositionedSearchableImagePDF(output, valid, overlayPages)
	profile.stage("PDF write", stage)
	if pdfErr == nil {
		cachePut(cacheDir, pdfCacheKey, output, cacheBytes)
	}
	return Result{Title: doc.Title, Output: output, Pages: len(valid)}, pdfErr
}

var (
	absImgTagRE = regexp.MustCompile(`(?is)<img\b[^>]*class=["']?absimg["']?[^>]*>`)
	clipRE      = regexp.MustCompile(`(?i)clip\s*:\s*rect\(\s*([\d.]+)px\s+([\d.]+)px\s+([\d.]+)px\s+([\d.]+)px`)
	urlRE       = regexp.MustCompile(`(?i)(?:orig|src)=["']([^"']+)["']`)
	canvasPool  sync.Pool
)

func pooledCanvas(width, height int) (*image.RGBA, func()) {
	size := width * height * 4
	var pixels []byte
	if cached := canvasPool.Get(); cached != nil {
		pixels, _ = cached.([]byte)
	}
	if cap(pixels) < size {
		pixels = make([]byte, size)
	} else {
		pixels = pixels[:size]
	}
	canvas := &image.RGBA{Pix: pixels, Stride: width * 4, Rect: image.Rect(0, 0, width, height)}
	return canvas, func() {
		if cap(pixels) <= 16<<20 {
			canvasPool.Put(pixels[:0])
		}
	}
}

// Some documents use a tall sprite sheet, so image tiles must be composed on
// the page's declared canvas.
func composePageImage(page Page, sources map[string]string, directory string) string {
	if page.Width <= 0 || page.Height <= 0 {
		return ""
	}
	tags := absImgTagRE.FindAllString(page.HTML, -1)
	if len(tags) == 0 {
		return ""
	}
	canvas, releaseCanvas := pooledCanvas(page.Width, page.Height)
	defer releaseCanvas()
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(image.White), image.Point{}, draw.Src)
	decoded := make(map[string]image.Image, len(tags))
	composed := false
	for _, tag := range tags {
		clip := clipRE.FindStringSubmatch(tag)
		urlMatch := urlRE.FindStringSubmatch(tag)
		if len(clip) != 5 || len(urlMatch) != 2 {
			continue
		}
		sourcePath := sources[html.UnescapeString(urlMatch[1])]
		if sourcePath == "" {
			sourcePath = sources[strings.Replace(html.UnescapeString(urlMatch[1]), "http://html.scribd.com", "https://html.scribdassets.com", 1)]
		}
		if sourcePath == "" {
			continue
		}
		source := decoded[sourcePath]
		if source == nil {
			file, err := os.Open(sourcePath)
			if err != nil {
				continue
			}
			source, _, err = image.Decode(file)
			_ = file.Close()
			if err != nil {
				continue
			}
			decoded[sourcePath] = source
		}
		ct, _ := strconv.Atoi(strings.Split(clip[1], ".")[0])
		cr, _ := strconv.Atoi(strings.Split(clip[2], ".")[0])
		cb, _ := strconv.Atoi(strings.Split(clip[3], ".")[0])
		cl, _ := strconv.Atoi(strings.Split(clip[4], ".")[0])
		style := ""
		if attrs := attributes(strings.TrimSuffix(strings.TrimPrefix(tag, "<img"), ">")); attrs["style"] != "" {
			style = attrs["style"]
		}
		css := styles(style)
		left, _ := pixels(css["left"])
		top, _ := pixels(css["top"])
		bounds := source.Bounds()
		if cl < 0 || ct < 0 || cr > bounds.Dx() || cb > bounds.Dy() || cr <= cl || cb <= ct {
			continue
		}
		dx, dy := int(left)+cl, int(top)+ct
		draw.Draw(canvas, image.Rect(dx, dy, dx+cr-cl, dy+cb-ct), source, image.Point{X: cl, Y: ct}, draw.Over)
		composed = true
	}
	if !composed {
		return ""
	}
	path := filepath.Join(directory, fmt.Sprintf("page-%03d-composed.png", page.Number))
	file, err := os.Create(path)
	if err != nil {
		return ""
	}
	defer file.Close()
	if err := png.Encode(file, canvas); err != nil {
		return ""
	}
	return path
}

// Scribd fonts are served as WOFF2 and converted to TTF when woff2_decompress
// is available; otherwise the PDF renderer uses its built-in font.
func fetchFonts(ctx context.Context, client *http.Client, assetPrefix string, pages []Page) map[string][]byte {
	if assetPrefix == "" {
		return nil
	}
	if _, err := exec.LookPath("woff2_decompress"); err != nil {
		return nil
	}
	families := map[string]bool{}
	for _, page := range pages {
		for _, item := range page.TextItems {
			if strings.HasPrefix(item.Family, "ff") {
				families[item.Family] = true
			}
		}
	}
	if len(families) == 0 {
		return nil
	}
	directory, err := os.MkdirTemp("", "unscribd-fonts-")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(directory)
	fonts := map[string][]byte{}
	type fontJob struct{ family, id, endpoint string }
	jobs := make(chan fontJob)
	results := make(chan struct {
		family string
		data   []byte
	}, len(families))
	var wg sync.WaitGroup
	workers := 6
	if len(families) < workers {
		workers = len(families)
	}
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				cacheKey := assetPrefix + "/" + job.id
				if data, ok := cachedFont(cacheKey); ok {
					results <- struct {
						family string
						data   []byte
					}{job.family, data}
					continue
				}
				data, status, err := request(ctx, client, http.MethodGet, job.endpoint, nil, map[string]string{"Referer": "https://www.scribd.com/"})
				if err != nil || status != http.StatusOK || len(data) < 100 {
					continue
				}
				woff := filepath.Join(directory, job.id+".woff2")
				if os.WriteFile(woff, data, 0600) != nil || exec.CommandContext(ctx, "woff2_decompress", woff).Run() != nil {
					continue
				}
				ttf, err := os.ReadFile(filepath.Join(directory, job.id+".ttf"))
				if err == nil && len(ttf) > 100 {
					cacheFont(cacheKey, ttf)
					results <- struct {
						family string
						data   []byte
					}{job.family, ttf}
				}
			}
		}()
	}
	for family := range families {
		id, err := strconv.Atoi(strings.TrimPrefix(family, "ff"))
		if err != nil {
			continue
		}
		name := fmt.Sprintf("%04d", id)
		jobs <- fontJob{family: family, id: name, endpoint: fmt.Sprintf("https://html.scribdassets.com/%s/fonts/%s.woff2", assetPrefix, name)}
	}
	close(jobs)
	wg.Wait()
	close(results)
	for result := range results {
		fonts[result.family] = result.data
	}
	return fonts
}

// WriteImagePDF writes a compact raster PDF. PNG input is decoded and embedded as JPEG.
