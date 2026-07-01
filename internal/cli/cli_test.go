package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/ilikosov/ranobelib/internal/model"
	"github.com/ilikosov/ranobelib/internal/progress"
)

func makeChapters() []model.Chapter {
	return []model.Chapter{
		{ID: 1, Volume: "1", Number: "1"},
		{ID: 2, Volume: "2", Number: "1"},
		{ID: 3, Volume: "3", Number: "1"},
	}
}

func run(input string) (*CLI, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return New(strings.NewReader(input), out), out
}

func TestPromptURLRetriesUntilNonEmpty(t *testing.T) {
	c, _ := run("\n\nhttps://ranobelib.me/ru/book/1--x\n")
	if got := c.PromptURL(); got != "https://ranobelib.me/ru/book/1--x" {
		t.Errorf("PromptURL = %q", got)
	}
}

func TestSelectVolumesAll(t *testing.T) {
	// 1 = все тома; 1 = один файл; 1 = с изображениями.
	c, _ := run("1\n1\n1\n")
	sel := c.SelectVolumes(makeChapters())
	if len(sel.Volumes) != 0 || sel.FirstN != 0 || sel.VolumeByVolume || sel.NoImages {
		t.Errorf("sel = %+v", sel)
	}
}

func TestSelectVolumesSpecificSplitNoImages(t *testing.T) {
	// 2 = конкретные тома; «3,1»; 2 = тома отдельно; 2 = без изображений.
	c, _ := run("2\n3,1\n2\n2\n")
	sel := c.SelectVolumes(makeChapters())
	if fmt.Sprint(sel.Volumes) != "[1 3]" {
		t.Errorf("Volumes = %v", sel.Volumes)
	}
	if !sel.VolumeByVolume || !sel.NoImages {
		t.Errorf("sel = %+v", sel)
	}
}

func TestSelectVolumesRange(t *testing.T) {
	// 3 = диапазон; «1-2»; 1 = один файл; 1 = с изображениями.
	c, _ := run("3\n1-2\n1\n1\n")
	sel := c.SelectVolumes(makeChapters())
	if fmt.Sprint(sel.Volumes) != "[1 2]" {
		t.Errorf("Volumes = %v", sel.Volumes)
	}
}

func TestSelectVolumesFirstN(t *testing.T) {
	// 4 = первые N; «2»; вопрос о разбиении не задаётся; 1 = с изображениями.
	c, _ := run("4\n2\n1\n")
	sel := c.SelectVolumes(makeChapters())
	if sel.FirstN != 2 || len(sel.Volumes) != 0 || sel.VolumeByVolume {
		t.Errorf("sel = %+v", sel)
	}
}

func TestSelectVolumesInvalidThenValid(t *testing.T) {
	// «9» — нет такого пункта; «2» + «7» — нет такого тома; «2» + «2» — ок.
	c, out := run("9\n2\n7\n2\n2\n1\n1\n")
	sel := c.SelectVolumes(makeChapters())
	if fmt.Sprint(sel.Volumes) != "[2]" {
		t.Errorf("Volumes = %v", sel.Volumes)
	}
	if !strings.Contains(out.String(), "❌") {
		t.Errorf("нет сообщений об ошибке ввода:\n%s", out.String())
	}
}

func TestSelectVolumesSingleVolumeSkipsSplitQuestion(t *testing.T) {
	// Один том в выборе → вопрос «один файл или по томам» не задаётся.
	c, out := run("2\n2\n1\n")
	sel := c.SelectVolumes(makeChapters())
	if fmt.Sprint(sel.Volumes) != "[2]" || sel.VolumeByVolume {
		t.Errorf("sel = %+v", sel)
	}
	if strings.Contains(out.String(), "Как сохранить книгу?") {
		t.Errorf("лишний вопрос о режиме сохранения:\n%s", out.String())
	}
}

func TestChooseSavedProgress(t *testing.T) {
	found := []progress.Saved{
		{BookID: "a--book", Data: model.Progress{CompletedCount: 3, Timestamp: "2026-01-01T00:00:00Z"}},
		{BookID: "b--book", Data: model.Progress{CompletedCount: 5, Timestamp: "2026-01-02T00:00:00Z"}},
	}

	c, _ := run("1\n2\n")
	if got := c.ChooseSavedProgress(found); got == nil || got.BookID != "b--book" {
		t.Errorf("ChooseSavedProgress = %+v", got)
	}

	c, _ = run("2\n")
	if got := c.ChooseSavedProgress(found); got != nil {
		t.Errorf("отказ должен вернуть nil, получено %+v", got)
	}

	c, _ = run("1\n")
	if got := c.ChooseSavedProgress(found[:1]); got == nil || got.BookID != "a--book" {
		t.Errorf("единственная сессия должна выбираться без вопроса: %+v", got)
	}

	if got := c.ChooseSavedProgress(nil); got != nil {
		t.Errorf("пустой список: %+v", got)
	}
}

func TestConfirmIncomplete(t *testing.T) {
	for input, want := range map[string]bool{"y\n": true, "да\n": true, "n\n": false, "\n": false} {
		c, _ := run(input)
		if got := c.ConfirmIncomplete(); got != want {
			t.Errorf("ConfirmIncomplete(%q) = %v", strings.TrimSpace(input), got)
		}
	}
}
