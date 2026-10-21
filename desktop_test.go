package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"everything/internal/music"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

type nextFunc func(context.Context, func(string)) (music.Result, error)

func (f nextFunc) Next(ctx context.Context, progress func(string)) (music.Result, error) {
	return f(ctx, progress)
}

func sampleResult() music.Result {
	return music.Result{
		Meta:         music.Meta{SongID: 42, SongName: "测试歌曲", Artist: "测试歌手", Album: "测试专辑"},
		Comment:      music.Comment{Content: "第一行热评[大笑]\n第二行 <img src=x> 😀", User: music.User{Nickname: "热评用户"}, LikedCount: 12000, TimeStr: "昨天"},
		PlaylistName: "测试歌单", Cat: "华语",
	}
}

// Run background updates on the test's UI thread, just as Window.Update does.
func pump(t *testing.T, queue <-chan func(), until func() bool) {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for !until() {
		select {
		case fn := <-queue:
			fn()
		case <-timeout.C:
			t.Fatal("background update timed out")
		}
	}
}

func TestNativeRefreshProgressFailureRetry(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	app := newDesktop(nextFunc(func(ctx context.Context, p func(string)) (music.Result, error) {
		n := calls.Add(1)
		p("正在抽歌…")
		select {
		case <-release:
		case <-ctx.Done():
			return music.Result{}, ctx.Err()
		}
		if n == 2 {
			return music.Result{}, errors.New("offline")
		}
		return sampleResult(), nil
	}))
	defer app.cancel()
	queue := make(chan func(), 32)
	app.post = func(fn func()) { queue <- fn }
	tt := ui.NewTester(app.view, 800, 700)
	if err := tt.Click("换一条"); err != nil {
		t.Fatal(err)
	}
	if !app.loading {
		t.Fatal("refresh did not start")
	}
	pump(t, queue, func() bool { return app.status == "正在抽歌…" })
	tt.Frame()
	if !tt.HasText("正在抽歌…") {
		t.Fatal(tt.Texts())
	}
	_ = tt.Click("换一条")
	if calls.Load() != 1 {
		t.Fatal("disabled button started another request")
	}
	close(release)
	pump(t, queue, func() bool { return !app.loading && !app.imagesLoading })
	tt.Frame()
	if !tt.HasText("测试歌曲") || !tt.HasText("♥ 1.2万 · 昨天") {
		t.Fatal(tt.Texts())
	}
	if commentSource(app.doc.Text()) != sampleResult().Comment.Content {
		t.Fatal("comment text changed")
	}
	if err := tt.Click("换一条"); err != nil {
		t.Fatal(err)
	}
	pump(t, queue, func() bool { return !app.loading })
	tt.Frame()
	if !tt.HasText("这次没抽到") || app.cover != nil || app.avatar != nil {
		t.Fatalf("failure state missing: loading=%v failure=%q calls=%d texts=%q", app.loading, app.failure, calls.Load(), tt.Texts())
	}
	if err := tt.Click("换一条"); err != nil {
		t.Fatal(err)
	}
	pump(t, queue, func() bool { return !app.loading && !app.imagesLoading })
	tt.Frame()
	if !tt.HasText("测试歌曲") || calls.Load() != 3 {
		t.Fatal("retry did not recover")
	}
}

func TestNativeCloseCancelsRequest(t *testing.T) {
	started, done := make(chan struct{}), make(chan struct{})
	app := newDesktop(nextFunc(func(ctx context.Context, _ func(string)) (music.Result, error) {
		close(started)
		<-ctx.Done()
		close(done)
		return music.Result{}, ctx.Err()
	}))
	queue := make(chan func(), 8)
	app.post = func(fn func()) { queue <- fn }
	app.next()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("not started")
	}
	app.cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("not cancelled")
	}

	// Wait for the completion callback, then prove it leaves closed state alone.
	select {
	case fn := <-queue:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("no completion")
	}
	if app.failure != "" || !app.loading {
		t.Fatal("closed UI was mutated")
	}
}

func TestNativeIgnoresStalePictures(t *testing.T) {
	var calls atomic.Int32
	app := newDesktop(nextFunc(func(context.Context, func(string)) (music.Result, error) {
		r := sampleResult()
		if calls.Add(1) == 1 {
			r.Meta.Cover = "old"
		}
		return r, nil
	}))
	defer app.cancel()
	queue := make(chan func(), 32)
	app.post = func(fn func()) { queue <- fn }
	oldStarted, oldRelease := make(chan struct{}), make(chan struct{})
	oldPicture, _ := ui.DecodeBitmap(mustEmojiPNG(t))
	app.loadImage = func(_ context.Context, url string) *ui.Bitmap {
		if url == "old" {
			close(oldStarted)
			<-oldRelease
			return oldPicture
		}
		return nil
	}
	app.next()
	pump(t, queue, func() bool { return !app.loading })
	select {
	case <-oldStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("image fetch did not start")
	}
	app.next()
	pump(t, queue, func() bool { return !app.loading && !app.imagesLoading })
	close(oldRelease)
	select {
	case fn := <-queue:
		fn()
	case <-time.After(5 * time.Second):
		t.Fatal("old image did not finish")
	}
	if app.cover != nil {
		t.Fatal("previous comment's picture replaced current picture")
	}
}

func mustEmojiPNG(t *testing.T) []byte {
	t.Helper()
	data, err := emojiFiles.ReadFile("assets/emoji/0.png")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestCommentEmojiAndLiteralText(t *testing.T) {
	data, err := emojiFiles.ReadFile("assets/emoji-map.json")
	if err != nil {
		t.Fatal(err)
	}
	var ids map[string]string
	if err := json.Unmarshal(data, &ids); err != nil {
		t.Fatal(err)
	}
	prefix := "😀👨‍👩‍👧‍👦\n[未知表情][constructor]<img src=x> **原文** "
	content := prefix
	for name := range ids {
		content += "[" + name + "]"
	}
	text := commentText(content)
	if commentSource(text) != content {
		t.Fatalf("text changed: %q", commentSource(text))
	}
	count := 0
	for _, run := range text.Runs(0, text.Len()) {
		if a := run.Attributes.Attachment; a != nil {
			count++
			if a.Image == nil || a.Width != 22 || a.Name == "" {
				t.Fatal("invalid emoji attachment")
			}
		}
	}
	if count != len(ids) {
		t.Fatalf("got %d images, want %d", count, len(ids))
	}
}

func TestNativeLayoutAndReadOnlyComment(t *testing.T) {
	for _, width := range []int{800, 390, 360} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			app := newDesktop(nil)
			defer app.cancel()
			app.result = sampleResult()
			app.result.Comment.Content = strings.Repeat("长评论[大笑]😀测试换行。\n", 35)
			app.doc.SetText(commentText(app.result.Comment.Content))
			app.doc.SetSelection(0, 0)
			tt := ui.NewTester(app.view, width, 700)
			for _, label := range []string{"歌曲封面", "评论内容", "换一条"} {
				r, ok := tt.Find(label)
				if !ok || r.W <= 0 || r.X < 0 || r.X+r.W > float32(width)+1 {
					t.Fatalf("%s outside window: %+v", label, r)
				}
			}
			before := app.doc.Text()
			if err := tt.Click("评论内容"); err != nil {
				t.Fatal(err)
			}
			tt.Type("不允许修改")
			if !app.doc.Text().Same(before) {
				t.Fatal("comment is editable")
			}
			if err := os.MkdirAll("test-results", 0755); err != nil {
				t.Fatal(err)
			}
			f, err := os.Create(fmt.Sprintf("test-results/native-layout-%d.png", width))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImageLoadingAndFallback(t *testing.T) {
	data := mustEmojiPNG(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/valid":
			_, _ = w.Write(data)
		case "/bad":
			_, _ = w.Write([]byte("not an image"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	previous := imageClient
	imageClient = server.Client()
	defer func() { imageClient = previous }()
	ctx := context.Background()
	if fetchImage(ctx, server.URL+"/valid") == nil {
		t.Fatal("valid image did not load")
	}
	for _, url := range []string{"", server.URL + "/bad", server.URL + "/missing"} {
		if fetchImage(ctx, url) != nil {
			t.Fatalf("bad image %q did not fall back", url)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if fetchImage(cancelled, server.URL+"/valid") != nil {
		t.Fatal("cancelled image loaded")
	}
}

// Reconstruct displayed text, including the alternative labels of inline images.
func commentSource(text richtext.Text) string {
	var b strings.Builder
	for _, run := range text.Runs(0, text.Len()) {
		if a := run.Attributes.Attachment; a != nil {
			b.WriteString(a.Name)
		} else {
			b.WriteString(run.Text)
		}
	}
	return b.String()
}

func TestRefreshKeepsLayoutAndRedHeart(t *testing.T) {
	for _, size := range []struct{ w, h int }{{800, 700}, {650, 640}, {390, 700}} {
		for _, content := range []string{"短评论[大笑]", strings.Repeat("一段较长的评论内容。\n", 25)} {
			t.Run(fmt.Sprintf("%dx%d/%d", size.w, size.h, len(content)), func(t *testing.T) {
				app := newDesktop(nextFunc(func(ctx context.Context, _ func(string)) (music.Result, error) {
					<-ctx.Done()
					return music.Result{}, ctx.Err()
				}))
				defer app.cancel()
				queue := make(chan func(), 8)
				app.post = func(fn func()) { queue <- fn }
				app.result = sampleResult()
				app.result.Comment.Content = content
				app.doc.SetText(commentText(content))
				app.doc.SetSelection(0, 0)
				app.status = "来自歌单《这是一个比较长的歌单名称，用于验证来源换行》· 分类 华语"
				tt := ui.NewTester(app.view, size.w, size.h)
				positions := map[string]ui.Rect{}
				for _, label := range []string{"歌曲封面", "评论者头像", "换一条"} {
					r, ok := tt.Find(label)
					if !ok {
						t.Fatalf("missing %s", label)
					}
					positions[label] = r
				}
				meta, ok := tt.Find("♥ 1.2万 · 昨天")
				if !ok {
					t.Fatal("missing likes")
				}
				redPixels := 0
				img := tt.Image()
				for y := int(meta.Y); y < int(meta.Y+meta.H); y++ {
					for x := int(meta.X); x < int(meta.X+meta.W); x++ {
						r, g, b, a := img.At(x, y).RGBA()
						if a > 0 && r > 30000 && r > g*3/2 && r > b*3/2 {
							redPixels++
						}
					}
				}
				if redPixels == 0 {
					t.Fatal("heart is not rendered red")
				}
				if err := tt.Click("换一条"); err != nil {
					t.Fatal(err)
				}
				if !app.loading {
					t.Fatal("refresh did not start")
				}
				for label, before := range positions {
					after, ok := tt.Find(label)
					if !ok || before != after {
						t.Fatalf("%s moved during loading: %+v -> %+v", label, before, after)
					}
				}
				if size.w == 650 && len(content) < 100 {
					f, err := os.Create("test-results/native-loading.png")
					if err != nil {
						t.Fatal(err)
					}
					defer f.Close()
					if err := png.Encode(f, tt.Image()); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestEmojiAlignsWithText(t *testing.T) {
	doc := richtext.NewDocument(commentText("田[大哭]田[大笑]田[可爱]田"))
	tt := ui.NewTester(func(c *ui.Context) {
		theme := *ui.DarkTheme()
		theme.Font = commentFontFamily
		c.SetTheme(&theme)
		ui.Column(c).Fill().Padding(12).Background(ui.Hex("#202024")).Children(func() {
			richtext.EditorBase(c, doc, richtext.EditorOptions{
				Label: "alignment", ReadOnly: true,
				Font: ui.Font{Family: commentFontFamily, Size: commentFontSize},
			}).FillWidth().Height(80)
		})
	}, 260, 110)
	img := tt.Image()
	if err := os.MkdirAll("test-results", 0755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create("test-results/emoji-alignment.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	// Compare the actual painted center of a square CJK glyph and the
	// adjacent yellow face, rather than only checking attachment sizes.
	glyphTop, glyphBottom, faceTop, faceBottom := 110, -1, 110, -1
	for y := 0; y < 110; y++ {
		for x := 12; x < 30; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 45000 && g > 45000 && b > 45000 {
				glyphTop = min(glyphTop, y)
				glyphBottom = max(glyphBottom, y)
			}
		}
		for x := 30; x < 52; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > 45000 && g > 25000 && b < 30000 {
				faceTop = min(faceTop, y)
				faceBottom = max(faceBottom, y)
			}
		}
	}
	if glyphBottom < 0 || faceBottom < 0 {
		t.Fatalf("missing glyph or face: glyph %d..%d face %d..%d", glyphTop, glyphBottom, faceTop, faceBottom)
	}
	delta := (glyphTop + glyphBottom) - (faceTop + faceBottom)
	if delta < -4 || delta > 4 {
		t.Fatalf("emoji center differs by %.1f px: glyph %d..%d face %d..%d", float64(delta)/2, glyphTop, glyphBottom, faceTop, faceBottom)
	}
}
