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

// NewNewsletter splits body into HTML paragraphs, escaping it and turning
// web addresses into links.
func NewNewsletter(subject, body string) Newsletter {
	body = strings.ReplaceAll(strings.TrimSpace(body), "\r\n", "\n")
	n := Newsletter{Subject: strings.TrimSpace(subject), Body: body}
	for _, para := range paraSep.Split(body, -1) {
		if para = strings.TrimSpace(para); para == "" {
			continue
		}
		escaped := html.EscapeString(para)
		linked := urlRE.ReplaceAllStringFunc(escaped, func(u string) string {
			return `<a href="` + u + `" style="color:#007994;">` + u + `</a>`
		})
		n.Paragraphs = append(n.Paragraphs, template.HTML(strings.ReplaceAll(linked, "\n", "<br>")))
	}
	return n
}
