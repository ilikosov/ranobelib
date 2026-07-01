// Package content преобразует содержимое глав из API в HTML.
//
// API отдаёт контент главы в одном из двух форматов:
//   - готовая HTML-строка;
//   - JSON-документ {"type":"doc","content":[...]} (формат редактора),
//     который нужно конвертировать в HTML, подставляя изображения из
//     списка attachments.
package content

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/ilikosov/ranobelib/internal/model"
)

// node — узел документа формата редактора.
type node struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Content []node          `json:"content"`
	Attrs   json.RawMessage `json:"attrs"`
	Marks   []struct {
		Type string `json:"type"`
	} `json:"marks"`
}

// ToHTML преобразует контент главы в HTML. Относительные ссылки на
// изображения абсолютизируются относительно siteURL.
func ToHTML(raw json.RawMessage, attachments []model.Attachment, siteURL string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}

	// Вариант 1: контент — готовая HTML-строка.
	var htmlStr string
	if err := json.Unmarshal(raw, &htmlStr); err == nil {
		return absolutizeImageSrc(htmlStr, siteURL), nil
	}

	// Вариант 2: контент — документ {"type":"doc", ...}.
	var doc node
	if err := json.Unmarshal(raw, &doc); err != nil {
		return "", fmt.Errorf("неизвестный формат контента главы: %w", err)
	}
	byName := make(map[string]model.Attachment, len(attachments))
	for _, a := range attachments {
		byName[a.Name] = a
	}
	var sb strings.Builder
	for _, child := range doc.Content {
		renderNode(&sb, child, byName, siteURL)
	}
	return sb.String(), nil
}

func renderNode(sb *strings.Builder, n node, attachments map[string]model.Attachment, siteURL string) {
	switch n.Type {
	case "paragraph":
		sb.WriteString("<p>")
		renderChildren(sb, n, attachments, siteURL)
		sb.WriteString("</p>\n")
	case "heading":
		level := headingLevel(n.Attrs)
		fmt.Fprintf(sb, "<h%d>", level)
		renderChildren(sb, n, attachments, siteURL)
		fmt.Fprintf(sb, "</h%d>\n", level)
	case "text":
		sb.WriteString(wrapMarks(html.EscapeString(n.Text), n))
	case "hardBreak":
		sb.WriteString("<br/>")
	case "horizontalRule", "delimiter":
		sb.WriteString("<hr/>\n")
	case "blockquote":
		sb.WriteString("<blockquote>")
		renderChildren(sb, n, attachments, siteURL)
		sb.WriteString("</blockquote>\n")
	case "bulletList":
		sb.WriteString("<ul>")
		renderChildren(sb, n, attachments, siteURL)
		sb.WriteString("</ul>\n")
	case "orderedList":
		sb.WriteString("<ol>")
		renderChildren(sb, n, attachments, siteURL)
		sb.WriteString("</ol>\n")
	case "listItem":
		sb.WriteString("<li>")
		renderChildren(sb, n, attachments, siteURL)
		sb.WriteString("</li>")
	case "image":
		renderImage(sb, n, attachments, siteURL)
	default:
		// Неизвестный узел — рендерим его содержимое, чтобы не терять текст.
		renderChildren(sb, n, attachments, siteURL)
	}
}

func renderChildren(sb *strings.Builder, n node, attachments map[string]model.Attachment, siteURL string) {
	for _, child := range n.Content {
		renderNode(sb, child, attachments, siteURL)
	}
}

func headingLevel(attrs json.RawMessage) int {
	var a struct {
		Level int `json:"level"`
	}
	if err := json.Unmarshal(attrs, &a); err == nil && a.Level >= 1 && a.Level <= 6 {
		return a.Level
	}
	return 2
}

func wrapMarks(text string, n node) string {
	for _, m := range n.Marks {
		switch m.Type {
		case "bold":
			text = "<strong>" + text + "</strong>"
		case "italic":
			text = "<em>" + text + "</em>"
		case "underline":
			text = "<u>" + text + "</u>"
		case "strike":
			text = "<s>" + text + "</s>"
		}
	}
	return text
}

func renderImage(sb *strings.Builder, n node, attachments map[string]model.Attachment, siteURL string) {
	var attrs struct {
		Images []struct {
			Image string `json:"image"`
		} `json:"images"`
	}
	if err := json.Unmarshal(n.Attrs, &attrs); err != nil {
		return
	}
	for _, img := range attrs.Images {
		att, ok := attachments[img.Image]
		if !ok {
			sb.WriteString("<p>📷 [изображение недоступно]</p>\n")
			continue
		}
		src := absolutizeURL(att.URL, siteURL)
		fmt.Fprintf(sb, `<div class="image-container"><img src="%s" alt="%s"/></div>`+"\n",
			html.EscapeString(src), html.EscapeString(att.Filename))
	}
}

func absolutizeURL(u, siteURL string) string {
	if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
		return u
	}
	return siteURL + "/" + strings.TrimPrefix(u, "/")
}

var (
	imgSrcRe = regexp.MustCompile(`(?i)(<img[^>]*\ssrc=")([^"]+)(")`)
	imgTagRe = regexp.MustCompile(`(?is)<img[^>]*>`)
	figureRe = regexp.MustCompile(`(?is)<figure[^>]*>.*?</figure>`)
)

// absolutizeImageSrc переписывает относительные src у <img> в HTML-строке.
func absolutizeImageSrc(htmlStr, siteURL string) string {
	return imgSrcRe.ReplaceAllStringFunc(htmlStr, func(match string) string {
		parts := imgSrcRe.FindStringSubmatch(match)
		return parts[1] + absolutizeURL(parts[2], siteURL) + parts[3]
	})
}

// RemoveImages вырезает из HTML все изображения (режим «без изображений»),
// заменяя их текстовым плейсхолдером — как в оригинальном парсере.
func RemoveImages(htmlStr string) string {
	htmlStr = figureRe.ReplaceAllString(htmlStr, "<p>📷 [изображение удалено]</p>")
	htmlStr = imgTagRe.ReplaceAllString(htmlStr, "<p>📷 [изображение удалено]</p>")
	return htmlStr
}
