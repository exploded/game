package mail

import (
	"html"
	"html/template"
	"regexp"
	"strings"
)

// Newsletter is the data for news.tmpl: plain text an admin typed, with
// blank lines between paragraphs.
type Newsletter struct {
	Subject    string
	Body       string
	Paragraphs []template.HTML
}

var (
	urlRE   = regexp.MustCompile(`https?://[^\s<>"']+[^\s<>"'.,;:!?)]`)
	paraSep = regexp.MustCompile(`\n\s*\n`)
)

// NewNewsletter builds a newsletter from what an admin typed.
func NewNewsletter(subject, body string) Newsletter {
	body = strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n")
	return Newsletter{Subject: strings.TrimSpace(subject), Body: body, Paragraphs: Paragraphs(body)}
}

// Paragraphs turns plain text into HTML paragraphs: it escapes the text,
// splits it at blank lines, keeps single line breaks and turns web addresses
// into links. The question pages use it too.
func Paragraphs(text string) []template.HTML {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\r\n", "\n")
	var out []template.HTML
	for _, para := range paraSep.Split(text, -1) {
		if para = strings.TrimSpace(para); para == "" {
			continue
		}
		escaped := html.EscapeString(para)
		linked := urlRE.ReplaceAllStringFunc(escaped, func(u string) string {
			return `<a href="` + u + `" style="color:#007994;" rel="nofollow noopener">` + u + `</a>`
		})
		out = append(out, template.HTML(strings.ReplaceAll(linked, "\n", "<br>")))
	}
	return out
}
