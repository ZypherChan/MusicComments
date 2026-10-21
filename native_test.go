package main

import (
	"fmt"
	"os"
	"testing"
	"time"

	"everything/internal/music"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Opt in: this opens a real native UI window and calls the live upstream API.
func TestMain(m *testing.M) {
	if os.Getenv("MUSIC_NATIVE_TEST") != "1" {
		os.Exit(m.Run())
	}
	if err := nativeLive(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

func nativeLive() error {
	result := make(chan error, 1)
	mygo.App.WhenReady(func() {
		mygo.Theme.SetSource(mygo.ThemeDark)
		app := newDesktop(music.NewClient())
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "MusicComments native verification", Width: 800, Height: 700,
			Content: ui.View(app.view),
		})
		app.post = win.Update
		win.OnClosed(app.cancel)
		app.next()
		go func() {
			defer mygo.App.Quit()
			defer app.cancel()
			deadline := time.NewTimer(90 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				select {
				case <-deadline.C:
					result <- fmt.Errorf("native live check timed out")
					return
				case <-app.ctx.Done():
					result <- app.ctx.Err()
					return
				case <-tick.C:
					type state struct {
						ready                  bool
						failure, song, content string
					}
					ch := make(chan state, 1)
					win.Update(func() {
						ch <- state{!app.loading && !app.imagesLoading, app.failure, app.result.Meta.SongName, app.result.Comment.Content}
					})
					var snapshot state
					select {
					case snapshot = <-ch:
					case <-deadline.C:
						result <- fmt.Errorf("UI update timed out")
						return
					}
					if !snapshot.ready {
						continue
					}
					if snapshot.failure != "" {
						result <- fmt.Errorf("live API: %s", snapshot.failure)
						return
					}
					if snapshot.song == "" {
						result <- fmt.Errorf("missing song")
						return
					}
					fmt.Printf("native UI live song: %s\ncomment: %s\n", snapshot.song, snapshot.content)
					if err := os.MkdirAll("test-results", 0755); err != nil {
						result <- err
						return
					}
					for _, size := range []struct {
						width, height int
						name          string
					}{
						{800, 700, "native-live.png"}, {390, 700, "native-narrow.png"},
					} {
						win.SetContentSize(size.width, size.height)
						png, err := win.CapturePage()
						if err != nil {
							result <- err
							return
						}
						if err := os.WriteFile("test-results/"+size.name, png, 0644); err != nil {
							result <- err
							return
						}
					}
					result <- nil
					return
				}
			}
		}()
	})
	if err := mygo.App.Run(); err != nil {
		return err
	}
	return <-result
}
