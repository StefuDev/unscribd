package unscribd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func ExtractDocumentID(value string) (string, error) {
	if match := docIDRE.FindStringSubmatch(value); len(match) == 2 {
		return match[1], nil
	}
	value = strings.TrimSpace(value)
	if value != "" {
		for _, r := range value {
			if r < '0' || r > '9' {
				return "", fmt.Errorf("cannot extract document id from: %s", value)
			}
		}
		return value, nil
	}
	return "", fmt.Errorf("cannot extract document id from: %s", value)
}

func SanitizeFilename(name string) string {
	name = strings.TrimSpace(invalidFileRE.ReplaceAllString(name, "_"))
	if len(name) > 100 {
		name = name[:100]
	}
	if name == "" {
		return "document"
	}
	return name
}

func NewClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 32
	transport.MaxIdleConnsPerHost = 16
	transport.MaxConnsPerHost = 16
	transport.IdleConnTimeout = 60 * time.Second
	return &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: transport}, nil
}

func request(ctx context.Context, client *http.Client, method, endpoint string, body io.Reader, headers map[string]string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

func FetchDocument(ctx context.Context, client *http.Client, id string) (Document, error) {
	endpoint := "https://www.scribd.com/document/" + id
	body, status, err := request(ctx, client, http.MethodGet, endpoint, nil, map[string]string{"Accept": "text/html,application/xhtml+xml"})
	if err != nil {
		return Document{}, err
	}
	if status != http.StatusOK {
		return Document{}, fmt.Errorf("GET %s returned %d", endpoint, status)
	}
	markup := string(body)
	if strings.Contains(markup, "Client Challenge") {
		return Document{}, fmt.Errorf("Scribd returned Client Challenge; Go does not provide curl_cffi TLS impersonation")
	}
	doc := parseDocument(id, endpoint, markup)
	if len(doc.Pages) == 0 {
		return Document{}, fmt.Errorf("no pages found; document may be restricted or parsing changed")
	}
	return doc, nil
}

func first(re *regexp.Regexp, value string) string {
	m := re.FindStringSubmatch(value)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}
func parseDocument(id, endpoint, markup string) Document {
	title := first(shortTitleRE, markup)
	if title == "" {
		title = first(titleRE, markup)
	}
	if title != "" {
		var decoded string
		if json.Unmarshal([]byte(`"`+title+`"`), &decoded) == nil {
			title = decoded
		}
	}
	if title == "" {
		title = strings.TrimSpace(html.UnescapeString(tagRE.ReplaceAllString(first(htmlTitleRE, markup), "")))
		title = strings.TrimSpace(strings.Split(strings.Split(title, "|")[0], " - ")[0])
	}
	if title == "" {
		title = "scribd-" + id
	}
	count, _ := strconv.Atoi(first(pageCountRE, markup))
	urls := contentRE.FindAllStringSubmatch(markup, -1)
	pages := make([]PageInfo, 0, len(urls))
	for i, m := range urls {
		if strings.Contains(m[1], "html.scribdassets.com") {
			pages = append(pages, PageInfo{Number: i + 1, ContentURL: m[1]})
		}
	}
	if count == 0 {
		count = len(pages)
	}
	return Document{ID: id, URL: endpoint, Title: title, AssetPrefix: first(assetRE, markup), DisplayType: first(displayRE, markup), PageCount: count, Pages: pages}
}

func appendToken(raw, token string) string {
	if token == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set("token", token)
	u.RawQuery = q.Encode()
	return u.String()
}
func FetchToken(ctx context.Context, client *http.Client, id, current string) string {
	payload := map[string]string{}
	if current != "" {
		payload["current_token"] = current
	}
	body, _ := json.Marshal(payload)
	data, status, err := request(ctx, client, http.MethodPost, "https://www.scribd.com/document/"+id+"/token", bytes.NewReader(body), map[string]string{"Content-Type": "application/json", "Origin": "https://www.scribd.com", "Referer": "https://www.scribd.com/document/" + id})
	if err != nil || status != http.StatusOK {
		return ""
	}
	var result struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(data, &result) != nil {
		return ""
	}
	return result.Token
}

func ParseJSONP(value string) (string, error) {
	value = strings.TrimSpace(value)
	raw := value
	if match := jsonpRE.FindStringSubmatch(value); len(match) == 2 {
		raw = "[" + match[1] + "]"
	}
	if !strings.HasPrefix(raw, "[") {
		return value, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return "", fmt.Errorf("parse JSONP: %w", err)
	}
	if len(values) == 0 {
		return "", nil
	}
	return values[0], nil
}
