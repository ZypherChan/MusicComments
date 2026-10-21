package music

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var categories = []string{"全部", "华语", "欧美", "日语", "韩语", "流行", "摇滚", "民谣", "电子", "说唱", "爵士", "古典", "轻音乐", "ACG", "影视原声", "怀旧", "清晨", "夜晚", "学习", "运动", "治愈", "R&B/Soul", "古风", "舞曲"}

type Meta struct {
	SongID   int64  `json:"songId"`
	SongName string `json:"songName"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Cover    string `json:"cover"`
}
type User struct {
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}
type Comment struct {
	Content    string `json:"content"`
	User       User   `json:"user"`
	LikedCount int64  `json:"likedCount"`
	TimeStr    string `json:"timeStr"`
}
type Result struct {
	Meta         Meta    `json:"meta"`
	Comment      Comment `json:"comment"`
	PlaylistName string  `json:"playlistName"`
	Cat          string  `json:"cat"`
}
type artist struct {
	Name string `json:"name"`
}
type album struct {
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}
type track struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	AR      []artist `json:"ar"`
	Artists []artist `json:"artists"`
	AL      *album   `json:"al"`
	Album   *album   `json:"album"`
}

func (t track) meta() Meta {
	ar := t.AR
	if ar == nil {
		ar = t.Artists
	}
	names := make([]string, 0, len(ar))
	for _, a := range ar {
		names = append(names, a.Name)
	}
	m := Meta{SongID: t.ID, SongName: t.Name, Artist: strings.Join(names, ", ")}
	al := t.AL
	if al == nil {
		al = t.Album
	}
	if al != nil {
		m.Album = al.Name
		m.Cover = strings.Replace(al.PicURL, "http://", "https://", 1)
	}
	return m
}

// Client makes requests directly to the original NetEase API, without a proxy.
type Client struct {
	http       *http.Client
	baseURL    string
	pick       func(int) int
	mu         sync.Mutex
	lastSongID int64
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 15 * time.Second}, baseURL: "http://music.163.com", pick: rand.IntN}
}
func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	r, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode < 200 || r.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", r.StatusCode)
	}
	return json.NewDecoder(r.Body).Decode(out)
}

// Next prefers classic playlists and popular comments, with a bounded search
// and a cached fallback so sparse playlists do not make refresh fail.
func (c *Client) Next(ctx context.Context, progress func(string)) (Result, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var lastErr error
	var best Result
	haveBest := false
	for attempt := 0; attempt < 5; attempt++ {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if err := searchCtx.Err(); err != nil {
			lastErr = err
			break
		}
		minLikes := int64(10000)
		if attempt >= 3 {
			minLikes = 1000
			if haveBest && best.Comment.LikedCount >= minLikes {
				c.lastSongID = best.Meta.SongID
				return best, nil
			}
		}
		r, err := c.fetchOne(searchCtx, progress, minLikes)
		if ctx.Err() != nil {
			return Result{}, ctx.Err()
		}
		if err == nil {
			if !haveBest || r.Comment.LikedCount > best.Comment.LikedCount {
				best, haveBest = r, true
			}
			if r.Comment.LikedCount >= minLikes {
				c.lastSongID = r.Meta.SongID
				return r, nil
			}
		}
		lastErr = err
	}
	if haveBest {
		c.lastSongID = best.Meta.SongID
		return best, nil
	}
	return Result{}, lastErr
}
func (c *Client) fetchOne(ctx context.Context, progress func(string), minLikes int64) (Result, error) {
	cat := "怀旧"
	if c.pick(10) >= 8 {
		cat = categories[c.pick(len(categories))]
	}
	offset := c.pick(16) * 35
	type playlist struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		TrackCount int    `json:"trackCount"`
	}
	var page struct {
		Playlists []playlist `json:"playlists"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/playlist/list?cat=%s&order=hot&offset=%d&limit=35", url.QueryEscape(cat), offset), &page); err != nil {
		return Result{}, err
	}
	list := page.Playlists[:0]
	for _, p := range page.Playlists {
		if p.TrackCount >= 20 {
			list = append(list, p)
		}
	}
	if len(list) == 0 {
		return Result{}, errors.New("empty playlist page")
	}
	// Prefer clearly named oldies collections without excluding other music.
	var classic []playlist
	for _, p := range list {
		for _, word := range []string{"经典", "老歌", "金曲", "怀旧", "年代", "70后", "80后", "90后"} {
			if strings.Contains(p.Name, word) {
				classic = append(classic, p)
				break
			}
		}
	}
	if len(classic) > 0 && c.pick(5) < 4 {
		list = classic
	}
	pl := list[c.pick(len(list))]
	if progress != nil {
		progress(fmt.Sprintf("歌单《%s》· 分类 %s，正在抽歌…", pl.Name, cat))
	}
	var detail struct {
		Playlist struct {
			Name     string `json:"name"`
			TrackIDs []struct {
				ID int64 `json:"id"`
			} `json:"trackIds"`
			Tracks []track `json:"tracks"`
		} `json:"playlist"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/v6/playlist/detail?id=%d", pl.ID), &detail); err != nil {
		return Result{}, err
	}
	ids := detail.Playlist.TrackIDs
	if len(ids) == 0 {
		return Result{}, errors.New("playlist has no tracks")
	}
	// Sample distinct songs so retries do not query the same song repeatedly.
	pool := make([]int64, 0, len(ids))
	seen := make(map[int64]bool)
	for _, track := range ids {
		if track.ID > 0 && !seen[track.ID] {
			pool = append(pool, track.ID)
			seen[track.ID] = true
		}
	}
	if len(pool) > 1 {
		for i, id := range pool {
			if id == c.lastSongID {
				pool = append(pool[:i], pool[i+1:]...)
				break
			}
		}
	}
	var best Result
	haveBest := false
	for i := 0; i < 6 && len(pool) > 0; i++ {
		if err := ctx.Err(); err != nil {
			if haveBest && !errors.Is(err, context.Canceled) {
				return best, nil
			}
			return Result{}, err
		}
		index := c.pick(len(pool))
		id := pool[index]
		pool[index] = pool[len(pool)-1]
		pool = pool[:len(pool)-1]
		meta, err := c.songMeta(ctx, id, detail.Playlist.Tracks)
		if err != nil {
			continue
		}
		comment, err := c.randomComment(ctx, id)
		if err != nil {
			continue
		}
		name := detail.Playlist.Name
		if name == "" {
			name = pl.Name
		}
		result := Result{Meta: meta, Comment: comment, PlaylistName: name, Cat: cat}
		if !haveBest || comment.LikedCount > best.Comment.LikedCount {
			best, haveBest = result, true
		}
		if comment.LikedCount >= minLikes {
			return result, nil
		}
	}
	if haveBest {
		return best, nil
	}
	return Result{}, errors.New("no song with comments in playlist")
}
func (c *Client) songMeta(ctx context.Context, id int64, embedded []track) (Meta, error) {
	for _, t := range embedded {
		if t.ID == id {
			return t.meta(), nil
		}
	}
	var d struct {
		Songs []track `json:"songs"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/song/detail?ids=[%d]", id), &d); err != nil {
		return Meta{}, err
	}
	if len(d.Songs) == 0 {
		return Meta{}, errors.New("song detail empty")
	}
	return d.Songs[0].meta(), nil
}
func (c *Client) randomComment(ctx context.Context, id int64) (Comment, error) {
	var d struct {
		HotComments []Comment `json:"hotComments"`
		Comments    []Comment `json:"comments"`
	}
	if err := c.get(ctx, fmt.Sprintf("/api/v1/resource/comments/R_SO_4_%d?limit=15&offset=0", id), &d); err != nil {
		return Comment{}, err
	}
	valid := func(items []Comment) []Comment {
		out := make([]Comment, 0, len(items))
		for _, v := range items {
			if v.Content != "" {
				out = append(out, v)
			}
		}
		return out
	}
	hot, recent := valid(d.HotComments), valid(d.Comments)
	for _, minLikes := range []int64{10000, 1000} {
		for _, items := range [][]Comment{hot, recent} {
			var popular []Comment
			for _, comment := range items {
				if comment.LikedCount >= minLikes {
					popular = append(popular, comment)
				}
			}
			if len(popular) > 0 {
				return popular[c.pick(len(popular))], nil
			}
		}
	}
	items := append(hot, recent...)
	if len(items) == 0 {
		return Comment{}, errors.New("no comments")
	}
	best := items[0]
	for _, comment := range items[1:] {
		if comment.LikedCount > best.LikedCount {
			best = comment
		}
	}
	return best, nil
}
