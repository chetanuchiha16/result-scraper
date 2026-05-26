package scraper

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Session wraps an HTTP client that persists VTU cookies across requests.
type Session struct {
	client  *http.Client
	formURL string // the POST target resolved from the results index page
}

var commonHeaders = map[string]string{
	"User-Agent":      "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36",
	"Accept-Language": "en-US,en;q=0.9",
	"Accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
}

// NewSession creates a Session with a persistent cookie jar and disabled TLS validation.
func NewSession() *Session {
	jar, _ := cookiejar.New(nil)
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &Session{
		client: &http.Client{
			Jar:       jar,
			Transport: tr,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return nil // follow redirects transparently
			},
		},
	}
}

// Close is a no-op placeholder for cleanup (useful if we add chromedp later).
func (s *Session) Close() {}

// FetchCaptcha loads the VTU results index page for the given USN,
// extracts the CAPTCHA image src, downloads the image, and returns
// a base64-encoded PNG string ready for the frontend <img> tag.
func (s *Session) FetchCaptcha(baseURL, usn string) (string, error) {
	// GET the index page to seed cookies
	req, _ := http.NewRequest("GET", baseURL, nil)
	applyHeaders(req)
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET index: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	pageHTML := string(body)

	// Parse the CAPTCHA image src from the HTML
	captchaSrc, err := extractCaptchaSrc(pageHTML, baseURL)
	if err != nil {
		return "", fmt.Errorf("parse captcha src: %w", err)
	}

	// Remember form action URL
	s.formURL = extractFormAction(pageHTML, baseURL)

	// Download CAPTCHA image
	imgReq, _ := http.NewRequest("GET", captchaSrc, nil)
	applyHeaders(imgReq)
	imgResp, err := s.client.Do(imgReq)
	if err != nil {
		return "", fmt.Errorf("download captcha: %w", err)
	}
	defer imgResp.Body.Close()
	imgBytes, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return "", err
	}

	return base64.StdEncoding.EncodeToString(imgBytes), nil
}

// SubmitCaptcha POSTs the USN + captcha text to the results form and
// returns the raw HTML of the result page.
func (s *Session) SubmitCaptcha(usn, captchaText string) (string, error) {
	target := s.formURL
	if target == "" {
		return "", fmt.Errorf("no form URL captured — call FetchCaptcha first")
	}

	formData := url.Values{}
	formData.Set("lns", usn)
	formData.Set("captchacode", captchaText)

	req, err := http.NewRequest("POST", target, strings.NewReader(formData.Encode()))
	if err != nil {
		return "", err
	}
	applyHeaders(req)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", target)

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("POST form: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	htmlStr := string(body)
	// Basic check: if we land back on the index page, CAPTCHA was wrong
	if strings.Contains(htmlStr, "captchacode") && !strings.Contains(htmlStr, "resultpage") {
		return "", fmt.Errorf("CAPTCHA rejected — please try again")
	}
	return htmlStr, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func applyHeaders(req *http.Request) {
	for k, v := range commonHeaders {
		req.Header.Set(k, v)
	}
}

// extractCaptchaSrc parses the HTML to find <img> tags whose src contains "captcha".
func extractCaptchaSrc(pageHTML, base string) (string, error) {
	doc, err := html.Parse(strings.NewReader(pageHTML))
	if err != nil {
		return "", err
	}
	var src string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "img" {
			for _, a := range n.Attr {
				if a.Key == "src" && strings.Contains(strings.ToLower(a.Val), "captcha") {
					src = a.Val
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if src == "" {
		return "", fmt.Errorf("captcha img not found in page")
	}
	// Resolve relative URLs
	if !strings.HasPrefix(src, "http") {
		base := strings.TrimRight(base[:strings.LastIndex(base, "/")+1], "/")
		src = base + "/" + strings.TrimLeft(src, "/")
	}
	return src, nil
}

// extractFormAction finds the <form action="..."> value.
func extractFormAction(pageHTML, base string) string {
	doc, _ := html.Parse(strings.NewReader(pageHTML))
	var action string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "form" {
			for _, a := range n.Attr {
				if a.Key == "action" {
					action = a.Val
					return
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	if action == "" {
		return base
	}
	if !strings.HasPrefix(action, "http") {
		dir := base[:strings.LastIndex(base, "/")+1]
		action = dir + strings.TrimLeft(action, "/")
	}
	return action
}
