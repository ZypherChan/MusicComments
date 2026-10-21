package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"everything/internal/music"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/richtext"
)

type commentClient interface {
	Next(context.Context, func(string)) (music.Result, error)
}

// All visible state lives on the UI thread; background work posts updates.
type desktop struct {
	client                 commentClient
	ctx                    context.Context
	cancel                 context.CancelFunc
	post                   func(func())
	result                 music.Result
	doc                    *richtext.Document
	loading, imagesLoading bool
	status, failure        string
	cover, avatar          *ui.Bitmap
	generation             uint64
	loadImage              func(context.Context, string) *ui.Bitmap
}

func newDesktop(client commentClient) *desktop {
	ctx, cancel := context.WithCancel(context.Background())
	return &desktop{client: client, ctx: ctx, cancel: cancel,
		doc: richtext.NewDocument(richtext.Text{}), loadImage: fetchImage}
}

func (app *desktop) next() {
	if app.loading || app.ctx.Err() != nil || app.post == nil {
		return
	}
	app.loading, app.failure = true, ""
	app.status = "正在随机抽取歌单…"
	app.generation++
	id := app.generation
	go func() {
		result, err := app.client.Next(app.ctx, func(status string) {
			app.post(func() {
				if app.ctx.Err() == nil && app.generation == id {
					app.status = status
				}
			})
		})
		app.post(func() {
			if app.ctx.Err() != nil || app.generation != id {
				return
			}
			app.loading = false
			app.cover, app.avatar = nil, nil
			app.result = result
			if err != nil {
				app.failure = "这次没抽到（接口不通），再点一次换一条试试。"
				app.status = ""
				app.imagesLoading = false
				app.doc.SetText(richtext.Text{})
				return
			}
			app.doc.SetText(commentText(result.Comment.Content))
			app.doc.SetSelection(0, 0)
			app.status = fmt.Sprintf("来自歌单《%s》· 分类 %s", result.PlaylistName, result.Cat)
			app.imagesLoading = true
			go app.loadPictures(id, result)
		})
	}()
}

func (app *desktop) loadPictures(id uint64, result music.Result) {
	var cover, avatar *ui.Bitmap
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); cover = app.loadImage(app.ctx, result.Meta.Cover) }()
	go func() { defer wg.Done(); avatar = app.loadImage(app.ctx, result.Comment.User.AvatarURL) }()
	wg.Wait()
	app.post(func() {
		if app.ctx.Err() == nil && app.generation == id {
			app.cover, app.avatar = cover, avatar
			app.imagesLoading = false
		}
	})
}

var imageClient = &http.Client{Timeout: 15 * time.Second}

func fetchImage(ctx context.Context, url string) *ui.Bitmap {
	if url == "" {
		return nil
	}
	url = strings.Replace(url, "http://", "https://", 1)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil
	}
	r, err := imageClient.Do(req)
	if err != nil {
		return nil
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return nil
	}
	const limit = 8 << 20
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil || len(data) > limit {
		return nil
	}
	bitmap, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bitmap
}

var (
	accent            = ui.Hex("#ec4141")
	muted             = ui.Hex("#a4a4a8")
	coverPlaceholder  = ui.MustParseSVG([]byte(`<svg width="80" height="80" viewBox="0 0 80 80"><defs><linearGradient id="g"><stop stop-color="#433554"/><stop offset="1" stop-color="#777f96"/></linearGradient></defs><rect width="80" height="80" rx="12" fill="url(#g)"/><circle cx="40" cy="40" r="28" fill="#18181b"/><circle cx="40" cy="40" r="19" fill="none" stroke="#48484f" stroke-width="2"/><circle cx="40" cy="40" r="8" fill="#ec4141"/><circle cx="40" cy="40" r="2" fill="#18181b"/></svg>`))
	avatarPlaceholder = ui.MustParseSVG([]byte(`<svg width="40" height="40" viewBox="0 0 40 40"><rect width="40" height="40" rx="20" fill="#555368"/><circle cx="20" cy="15" r="7" fill="#d9d9df"/><path d="M5 40 Q7 26 20 26 Q33 26 35 40" fill="#d9d9df"/></svg>`))
)

func (app *desktop) view(c *ui.Context) {
	theme := *ui.DarkTheme()
	theme.Accent = accent
	theme.AccentHover = ui.Hex("#ef5050")
	theme.AccentPressed = ui.Hex("#d93c3c")
	theme.Font = commentFontFamily
	theme.Text = ui.Hex("#eeeeef")
	c.SetTheme(&theme)
	w, h := c.Size()
	pad := float32(28)
	if w < 500 {
		pad = 20
	}
	cardWidth := min(float32(560), w-2*pad)
	commentHeight := max(float32(100), min(float32(220), h-350))
	if app.failure == "" {
		var spans []ui.Span
		text := app.doc.Text()
		for _, run := range text.Runs(0, text.Len()) {
			span := ui.Span{Text: run.Text, Font: theme.Font, Size: commentFontSize}
			if a := run.Attributes.Attachment; a != nil {
				span.ObjectWidth, span.ObjectHeight = a.Width, a.Height
			}
			spans = append(spans, span)
		}
		_, measured := c.MeasureText(max(1, cardWidth-2*pad-28), spans...)
		commentHeight = min(commentHeight, max(60, measured+20))
	}
	ui.Box(c).Fill().Background(ui.Hex("#19191c")).Clip().Children(func() {
		if app.cover != nil {
			ui.Image(c, app.cover).Absolute().Top(-60).Left(-60).Width(w + 120).Height(h + 120).
				Fit(ui.Cover).Blur(40).Opacity(0.22).PassThrough()
		}
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).MinHeight(h).FillWidth().Padding(pad).Center().Gap(28).Children(func() {
				ui.Text(c, "网 易 云 热 评").FontSize(15).LetterSpacing(5).TextColor(ui.Hex("#bbbbc0")).TextAlign(ui.Center)
				ui.Column(c).Width(cardWidth).Padding(pad).Gap(24).Radius(20).
					Background(ui.RGBA(35, 35, 38, 0.96)).Border(1, ui.Hex("#38383d")).
					Shadow(0, 16, 40, 0, ui.RGBA(0, 0, 0, 0.25)).Children(func() {
					ui.Row(c).Gap(16).Children(func() {
						var cover ui.ImageSource = coverPlaceholder
						if app.cover != nil {
							cover = app.cover
						}
						ui.Image(c, cover).Size(68, 68).Fit(ui.Cover).Radius(10).Label("歌曲封面").Shrink(0)
						ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
							name := app.result.Meta.SongName
							if name == "" {
								name = "随机热评"
							}
							ui.Text(c, name).FontSize(21).Bold().MaxLines(2)
							artist := app.result.Meta.Artist
							if app.result.Meta.Album != "" {
								artist += " · 《" + app.result.Meta.Album + "》"
							}
							ui.Text(c, artist).FontSize(13).TextColor(muted).MaxLines(2)
						})
					})
					ui.Divider(c).Background(ui.Hex("#38383d"))
					ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
						if app.loading || app.failure != "" {
							text := "正在随机抽取歌单…"
							if app.failure != "" {
								text = app.failure
							}
							ui.Text(c, text).Height(commentHeight).Grow(1).MinWidth(0).FontSize(17).TextColor(muted)
						} else {
							ui.Text(c, "“").FontSize(26).Bold().TextColor(accent)
							richtext.EditorBase(c.Key(app.generation), app.doc, richtext.EditorOptions{
								Label: "评论内容", ReadOnly: true, NoMarkdown: true,
								Font: ui.Font{Family: theme.Font, Size: commentFontSize},
							}).Height(commentHeight).Grow(1).MinWidth(0)
						}
					})
					ui.Row(c).Gap(10).Wrap().Children(func() {
						var avatar ui.ImageSource = avatarPlaceholder
						if app.avatar != nil {
							avatar = app.avatar
						}
						ui.Image(c, avatar).Size(32, 32).Fit(ui.Cover).Radius(16).Label("评论者头像")
						ui.Text(c, app.result.Comment.User.Nickname).FontSize(13).Grow(1).MinWidth(0).MaxLines(2)
						if app.failure == "" && app.result.Meta.SongName != "" {
							meta := formatLikes(app.result.Comment.LikedCount)
							if app.result.Comment.TimeStr != "" {
								meta += " · " + app.result.Comment.TimeStr
							}
							ui.RichText(c, ui.Span{Text: "♥ ", Color: accent}, ui.Span{Text: meta}).FontSize(12).TextColor(muted)
						}
					})
				})
				label := "⟳  换一条"
				if app.loading {
					label = "正在抽取…"
				}
				ui.PrimaryButton(c.Key("refresh"), label).Label("换一条").Disabled(app.loading).
					Width(136).Height(46).Radius(23).FontSize(15).OnClick(app.next)
				ui.Text(c, app.status).Width(cardWidth).Height(48).TextAlign(ui.Center).FontSize(12).TextColor(muted).MaxLines(3)
			})
		})
	})
}

func formatLikes(n int64) string {
	if n < 10000 {
		return fmt.Sprint(n)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/10000), ".0") + "万"
}
