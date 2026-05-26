package scraper

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// RenderHTMLToPDF takes the raw HTML string of a VTU results page,
// renders it headlessly via chromedp (using the user's installed Chrome),
// and prints it to a PDF file at pdfPath with standard A4 layout.
func RenderHTMLToPDF(htmlContent, pdfPath string) error {
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()

	ctx, cancelTimeout := context.WithTimeout(ctx, 30*time.Second)
	defer cancelTimeout()

	// Encode the HTML as a data: URL so chromedp can navigate to it directly
	// without needing a local server.
	encoded := "data:text/html;charset=utf-8," + urlEncodeHTML(htmlContent)

	var pdfBuf []byte
	err := chromedp.Run(ctx,
		chromedp.Navigate(encoded),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdfBuf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27).   // A4
				WithPaperHeight(11.69). // A4
				WithMarginTop(0.4).
				WithMarginBottom(0.4).
				WithMarginLeft(0.4).
				WithMarginRight(0.4).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		return fmt.Errorf("chromedp PDF render: %w", err)
	}

	return os.WriteFile(pdfPath, pdfBuf, 0644)
}

// urlEncodeHTML does minimal percent-encoding to embed raw HTML in a data: URI.
func urlEncodeHTML(s string) string {
	r := strings.NewReplacer(
		"%", "%25",
		"#", "%23",
		" ", "%20",
		"\n", "%0A",
		"\r", "",
	)
	return r.Replace(s)
}
