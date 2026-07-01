package content

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ilikosov/ranobelib/internal/model"
)

const site = "https://ranobelib.me"

func TestToHTMLPlainString(t *testing.T) {
	raw := json.RawMessage(`"<p>Привет</p><img src=\"/uploads/pic.jpg\">"`)
	got, err := ToHTML(raw, nil, site)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "<p>Привет</p>") {
		t.Errorf("нет абзаца: %q", got)
	}
	if !strings.Contains(got, `src="https://ranobelib.me/uploads/pic.jpg"`) {
		t.Errorf("относительный src не абсолютизирован: %q", got)
	}
}

func TestToHTMLDocFormat(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "doc",
		"content": [
			{"type": "heading", "attrs": {"level": 3}, "content": [{"type": "text", "text": "Глава 1"}]},
			{"type": "paragraph", "content": [
				{"type": "text", "text": "Обычный, "},
				{"type": "text", "text": "жирный", "marks": [{"type": "bold"}]},
				{"type": "text", "text": " и <опасный>", "marks": [{"type": "italic"}]}
			]},
			{"type": "horizontalRule"},
			{"type": "image", "attrs": {"images": [{"image": "att1"}, {"image": "missing"}]}}
		]
	}`)
	attachments := []model.Attachment{{Name: "att1", Filename: "pic.jpg", URL: "/uploads/pic.jpg"}}

	got, err := ToHTML(raw, attachments, site)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<h3>Глава 1</h3>",
		"<strong>жирный</strong>",
		"<em> и &lt;опасный&gt;</em>",
		"<hr/>",
		`<img src="https://ranobelib.me/uploads/pic.jpg" alt="pic.jpg"/>`,
		"[изображение недоступно]",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("в результате нет %q\nполный HTML:\n%s", want, got)
		}
	}
}

func TestToHTMLEmpty(t *testing.T) {
	got, err := ToHTML(nil, nil, site)
	if err != nil || got != "" {
		t.Errorf("got %q, err %v", got, err)
	}
}

func TestToHTMLUnknownNodeKeepsText(t *testing.T) {
	raw := json.RawMessage(`{"type":"doc","content":[
		{"type":"exotic","content":[{"type":"text","text":"не потеряй меня"}]}
	]}`)
	got, err := ToHTML(raw, nil, site)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "не потеряй меня") {
		t.Errorf("текст неизвестного узла потерян: %q", got)
	}
}

func TestRemoveImages(t *testing.T) {
	in := `<p>до</p><figure><img src="a.jpg"><figcaption>x</figcaption></figure><img src="b.png"/><p>после</p>`
	got := RemoveImages(in)
	if strings.Contains(got, "<img") || strings.Contains(got, "<figure") {
		t.Errorf("изображения не удалены: %q", got)
	}
	if !strings.Contains(got, "<p>до</p>") || !strings.Contains(got, "<p>после</p>") {
		t.Errorf("текст повреждён: %q", got)
	}
	if !strings.Contains(got, "изображение удалено") {
		t.Errorf("нет плейсхолдера: %q", got)
	}
}
