// Package model содержит общие структуры данных парсера.
package model

import "encoding/json"

// BookInfo — метаданные книги с ranobelib.me.
type BookInfo struct {
	Title       string // название (rus_name, если есть, иначе name)
	Author      string // автор(ы) через запятую
	CoverURL    string // абсолютный URL обложки
	Description string // краткое описание (summary)
}

// Branch — ветка перевода главы.
type Branch struct {
	ID       int64 `json:"id"`
	BranchID int64 `json:"branch_id"`
}

// Chapter — глава из списка глав книги.
type Chapter struct {
	ID       int64    `json:"id"`
	Volume   string   `json:"volume"`
	Number   string   `json:"number"`
	Name     string   `json:"name"`
	Branches []Branch `json:"branches"`
}

// Title возвращает человекочитаемый заголовок главы:
// «Том 1 Глава 2 — Название».
func (c Chapter) Title() string {
	title := "Том " + c.Volume + " Глава " + c.Number
	if c.Name != "" {
		title += " — " + c.Name
	}
	return title
}

// Attachment — вложение (изображение) главы.
type Attachment struct {
	Name     string `json:"name"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
}

// ChapterContent — контент одной главы из API.
// Content может быть либо HTML-строкой, либо JSON-документом
// {type:"doc", content:[...]}, поэтому хранится как RawMessage.
type ChapterContent struct {
	Content     json.RawMessage `json:"content"`
	Attachments []Attachment    `json:"attachments"`
}

// DownloadedChapter — скачанная глава: заголовок и готовый HTML.
type DownloadedChapter struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Data  string `json:"data"`
}

// Progress — сохранённое состояние загрузки книги.
type Progress struct {
	Timestamp      string              `json:"timestamp"`
	URL            string              `json:"url,omitempty"`
	CompletedCount int                 `json:"completedCount"`
	Chapters       []DownloadedChapter `json:"chapters"`
	AllChapters    []Chapter           `json:"allChapters,omitempty"`
}
