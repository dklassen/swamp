package greenhouse

import (
	"strings"

	xhtml "golang.org/x/net/html"
)

// blockTags are the elements whose closing tag should start a new line in
// stripHTML's output -- roughly, elements a browser would render on their
// own line. Everything else's text just concatenates within the current
// line.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "tr": true,
}

// stripHTML converts Greenhouse's HTML job description into a best-effort
// plain-text approximation, since Greenhouse's API (unlike Ashby's) only
// provides HTML, not a separate plain-text field. Uses a real HTML
// tokenizer rather than a regex: entities need decoding, and <script>/
// <style> content must never leak into the output, both of which a naive
// tag-stripping regex gets wrong.
func stripHTML(s string) string {
	z := xhtml.NewTokenizer(strings.NewReader(s))
	var b strings.Builder
	skipDepth := 0

	for {
		tt := z.Next()
		if tt == xhtml.ErrorToken {
			break
		}
		tag, _ := z.TagName()
		name := string(tag)

		switch tt {
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			if name == "script" || name == "style" {
				skipDepth++
			}
		case xhtml.EndTagToken:
			switch {
			case name == "script" || name == "style":
				if skipDepth > 0 {
					skipDepth--
				}
			case blockTags[name]:
				b.WriteString("\n")
			}
		case xhtml.TextToken:
			if skipDepth == 0 {
				b.Write(z.Text())
			}
		}
	}

	var lines []string
	for _, line := range strings.Split(b.String(), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return strings.Join(lines, "\n")
}
