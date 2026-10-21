package main

import (
	"log"

	"everything/internal/music"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	mygo.App.WhenReady(func() {
		mygo.Theme.SetSource(mygo.ThemeDark)
		app := newDesktop(music.NewClient())
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "网易云随机热评", Width: 800, Height: 700,
			MinWidth: 360, MinHeight: 480, BackgroundColor: "#141414",
			StateKey: "main", Content: ui.View(app.view),
		})
		app.post = win.Update
		win.OnClosed(app.cancel)
		app.next()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
