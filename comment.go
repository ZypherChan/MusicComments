package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sync"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

//go:embed assets/emoji-map.json assets/emoji/*.png
var emojiFiles embed.FS

var emojiPattern = regexp.MustCompile(`\[([^\[\]\r\n]+)\]`)
var emojiOnce sync.Once
var emojiImages map[string]*richtext.Attachment

const (
	commentFontFamily = "Microsoft YaHei UI, Segoe UI"
	commentFontSize   = 18
	commentEmojiSize  = 22
)

func loadEmoji() {
	emojiImages = make(map[string]*richtext.Attachment)
	data, err := emojiFiles.ReadFile("assets/emoji-map.json")
	if err != nil {
		panic(err)
	}
	var ids map[string]string
	if err := json.Unmarshal(data, &ids); err != nil {
		panic(err)
	}
	metrics := (ui.Font{Family: commentFontFamily, Size: commentFontSize}).Metrics()
	for name, id := range ids {
		data, err := emojiFiles.ReadFile(fmt.Sprintf("assets/emoji/%s.png", id))
		if err != nil {
			continue
		}
		bitmap, err := ui.DecodeBitmap(data)
		if err != nil {
			continue
		}
		emojiImages[name] = &richtext.Attachment{
			Image: bitmap, Width: commentEmojiSize, Height: commentEmojiSize, Name: "[" + name + "]",
			Draw: func(p *ui.Painter, r ui.Rect) {
				// MyGo places inline images above the baseline. Center the
				// image in the font's ascent/descent box instead.
				r.Y += (r.H + metrics.Descent - metrics.Ascent) / 2
				p.Image(bitmap, r, ui.Contain)
			},
		}
	}
}

// Plain runs preserve user text literally; only known labels become images.
func commentText(content string) richtext.Text {
	emojiOnce.Do(loadEmoji)
	var runs []richtext.Run
	offset := 0
	for _, match := range emojiPattern.FindAllStringSubmatchIndex(content, -1) {
		attachment := emojiImages[content[match[2]:match[3]]]
		if attachment == nil {
			continue
		}
		runs = append(runs, richtext.Run{Text: content[offset:match[0]]}, richtext.Run{
			Text: string(richtext.ObjectReplacement), Attributes: richtext.Attributes{Attachment: attachment},
		})
		offset = match[1]
	}
	runs = append(runs, richtext.Run{Text: content[offset:]})
	return richtext.TextOf(richtext.Paragraph{Runs: runs})
}
