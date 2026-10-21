package music

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fixtureClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	c := NewClient()
	c.baseURL = s.URL
	c.pick = func(int) int { return 0 }
	return c
}

func TestNext(t *testing.T) {
	for _, embedded := range []bool{true, false} {
		for _, hot := range []bool{true, false} {
			t.Run(fmt.Sprintf("embedded=%t/hot=%t", embedded, hot), func(t *testing.T) {
				var paths []string
				c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
					paths = append(paths, r.URL.RequestURI())
					switch r.URL.Path {
					case "/api/playlist/list":
						if r.URL.Query().Get("limit") != "35" || r.URL.Query().Get("offset") != "0" || r.URL.Query().Get("cat") != "怀旧" {
							t.Error("incorrect playlist query")
						}
						fmt.Fprint(w, `{"playlists":[{"id":1,"name":"忽略","trackCount":19},{"id":2,"name":"列表名","trackCount":20}]}`)
					case "/api/v6/playlist/detail":
						if r.URL.Query().Get("id") != "2" {
							t.Error("playlist filter failed")
						}
						tracks := `[]`
						if embedded {
							tracks = `[{"id":42,"name":"歌名","ar":[{"name":"歌手"}],"al":{"name":"专辑","picUrl":"http://example.com/cover"}}]`
						}
						fmt.Fprintf(w, `{"playlist":{"name":"详情名","trackIds":[{"id":42}],"tracks":%s}}`, tracks)
					case "/api/song/detail":
						if embedded {
							t.Error("embedded song should avoid metadata request")
						}
						if r.URL.Query().Get("ids") != "[42]" {
							t.Error("numeric track ID lost")
						}
						fmt.Fprint(w, `{"songs":[{"id":42,"name":"歌名","artists":[{"name":"歌手"}],"album":{"name":"专辑","picUrl":"http://example.com/cover"}}]}`)
					case "/api/v1/resource/comments/R_SO_4_42":
						if hot {
							fmt.Fprint(w, `{"hotComments":[null,{"content":""},{"content":"热评","user":{"nickname":"用户"},"likedCount":12000}],"comments":[{"content":"最新"}]}`)
						} else {
							fmt.Fprint(w, `{"hotComments":[null],"comments":[{"content":"最新"}]}`)
						}
					default:
						t.Errorf("unexpected path %s", r.URL)
						http.NotFound(w, r)
					}
				})
				var progress string
				r, err := c.Next(context.Background(), func(s string) { progress = s })
				if err != nil {
					t.Fatal(err)
				}
				want := "最新"
				if hot {
					want = "热评"
				}
				if r.Comment.Content != want || r.Meta.SongID != 42 || r.Meta.Cover != "https://example.com/cover" || r.Meta.Artist != "歌手" || r.PlaylistName != "详情名" || c.lastSongID != 42 {
					t.Fatalf("incorrect result: %+v", r)
				}
				if !strings.Contains(progress, "列表名") {
					t.Fatalf("missing progress: %s", progress)
				}
				wantCalls := 3
				if !embedded {
					wantCalls = 4
				}
				if !hot {
					wantCalls *= 5
				}
				if len(paths) != wantCalls {
					t.Fatalf("calls=%v", paths)
				}
			})
		}
	}
}

func TestRetryLimits(t *testing.T) {
	var playlists, comments int
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlist/list":
			playlists++
			fmt.Fprint(w, `{"playlists":[{"id":1,"trackCount":20}]}`)
		case "/api/v6/playlist/detail":
			fmt.Fprint(w, `{"playlist":{"trackIds":[{"id":42},{"id":43},{"id":44},{"id":45},{"id":46},{"id":47},{"id":48}],"tracks":[{"id":42},{"id":43},{"id":44},{"id":45},{"id":46},{"id":47},{"id":48}]}}`)
		default:
			comments++
			fmt.Fprint(w, `{"hotComments":[],"comments":[]}`)
		}
	})
	if _, err := c.Next(context.Background(), nil); err == nil {
		t.Fatal("expected failure")
	}
	if playlists != 5 || comments != 30 {
		t.Fatalf("playlist attempts=%d, song attempts=%d", playlists, comments)
	}
}

func TestRequestFailureAndCancellation(t *testing.T) {
	for _, body := range []string{"HTTP", "invalid JSON"} {
		t.Run(body, func(t *testing.T) {
			calls := 0
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if body == "HTTP" {
					w.WriteHeader(503)
				}
				fmt.Fprint(w, "invalid")
			})
			if _, err := c.Next(context.Background(), nil); err == nil {
				t.Fatal("expected error")
			}
			if calls != 5 {
				t.Fatalf("calls=%d", calls)
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, err := c.Next(ctx, nil); err != context.Canceled {
				t.Fatalf("cancellation=%v", err)
			}
			if calls != 5 {
				t.Fatal("canceled request reached server")
			}
		})
	}
}

func TestClassicPlaylistPreference(t *testing.T) {
	for _, prefer := range []bool{true, false} {
		t.Run(fmt.Sprint(prefer), func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/playlist/list":
					wantCat := "怀旧"
					if !prefer {
						wantCat = "全部"
					}
					if r.URL.Query().Get("cat") != wantCat {
						t.Errorf("category=%s", r.URL.Query().Get("cat"))
					}
					fmt.Fprint(w, `{"playlists":[{"id":1,"name":"本周新歌","trackCount":20},{"id":2,"name":"经典老歌金曲","trackCount":20}]}`)
				case "/api/v6/playlist/detail":
					wantID := "2"
					if !prefer {
						wantID = "1"
					}
					if r.URL.Query().Get("id") != wantID {
						t.Errorf("playlist=%s", r.URL.Query().Get("id"))
					}
					fmt.Fprint(w, `{"playlist":{"trackIds":[{"id":42}],"tracks":[{"id":42}]}}`)
				default:
					fmt.Fprint(w, `{"hotComments":[{"content":"经典评论","likedCount":15000}]}`)
				}
			})
			if !prefer {
				c.pick = func(n int) int {
					if n == 10 {
						return 8
					}
					if n == 5 {
						return 4
					}
					return 0
				}
			}
			if _, err := c.Next(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommentPopularityTiers(t *testing.T) {
	cases := []struct {
		name, body, want string
		last             bool
	}{
		{"ten-thousand", `{"hotComments":[{"content":"低赞","likedCount":1},{"content":"万赞甲","likedCount":10000},{"content":"万赞乙","likedCount":25000}]}`, "万赞甲", false},
		{"random-within-tier", `{"hotComments":[{"content":"万赞甲","likedCount":10000},{"content":"万赞乙","likedCount":25000}]}`, "万赞乙", true},
		{"recent-high-beats-hot-low", `{"hotComments":[{"content":"千赞","likedCount":2000}],"comments":[{"content":"新万赞","likedCount":12000}]}`, "新万赞", false},
		{"thousand", `{"hotComments":[{"content":"低赞","likedCount":999},{"content":"千赞","likedCount":1000}]}`, "千赞", false},
		{"best-low-fallback", `{"hotComments":[{"content":"低赞","likedCount":1}],"comments":[{"content":"较高","likedCount":900}]}`, "较高", false},
		{"empty-popular-ignored", `{"hotComments":[{"content":"","likedCount":99999},{"content":"有效","likedCount":1000}]}`, "有效", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) })
			if tc.last {
				c.pick = func(n int) int { return n - 1 }
			}
			got, err := c.randomComment(context.Background(), 42)
			if err != nil || got.Content != tc.want {
				t.Fatalf("comment=%+v err=%v", got, err)
			}
		})
	}
}

func TestSearchPopularityFallback(t *testing.T) {
	for _, tc := range []struct {
		name                string
		jackpot             bool
		likes               int64
		playlists, comments int
	}{
		{"keep-searching-for-ten-thousand", true, 20000, 3, 13},
		{"reuse-best-thousand-after-three-rounds", false, 1500, 3, 18},
		{"best-low-after-all-rounds", false, 900, 5, 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			playlists, comments := 0, 0
			c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/playlist/list":
					playlists++
					fmt.Fprintf(w, `{"playlists":[{"id":%d,"name":"经典老歌","trackCount":20}]}`, playlists)
				case "/api/v6/playlist/detail":
					fmt.Fprint(w, `{"playlist":{"trackIds":[{"id":42},{"id":43},{"id":44},{"id":45},{"id":46},{"id":47}],"tracks":[{"id":42},{"id":43},{"id":44},{"id":45},{"id":46},{"id":47}]}}`)
				default:
					comments++
					likes := tc.likes
					if tc.jackpot && playlists < 3 {
						likes = 1500
					}
					fmt.Fprintf(w, `{"hotComments":[{"content":"合成测试评论","likedCount":%d}]}`, likes)
				}
			})
			got, err := c.Next(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if got.Comment.LikedCount != tc.likes || playlists != tc.playlists || comments != tc.comments {
				t.Fatalf("likes=%d playlists=%d comments=%d", got.Comment.LikedCount, playlists, comments)
			}
			if c.lastSongID != got.Meta.SongID {
				t.Fatal("last song is not the returned fallback")
			}
		})
	}
}

func TestSongSamplingDoesNotRepeatOrReuseLastSong(t *testing.T) {
	var songs []string
	c := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/playlist/list":
			fmt.Fprint(w, `{"playlists":[{"id":1,"name":"经典","trackCount":20}]}`)
		case "/api/v6/playlist/detail":
			fmt.Fprint(w, `{"playlist":{"trackIds":[{"id":42},{"id":42},{"id":43},{"id":44}],"tracks":[{"id":42},{"id":43},{"id":44}]}}`)
		default:
			songs = append(songs, r.URL.Path)
			fmt.Fprint(w, `{"hotComments":[{"content":"低赞兜底","likedCount":5}]}`)
		}
	})
	c.lastSongID = 42
	if _, err := c.fetchOne(context.Background(), nil, 10000); err != nil {
		t.Fatal(err)
	}
	if len(songs) != 2 || songs[0] == songs[1] || strings.HasSuffix(songs[0], "_42") || strings.HasSuffix(songs[1], "_42") {
		t.Fatalf("invalid sampling: %v", songs)
	}
}
