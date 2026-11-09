// Package vimeo reads Consortium's Vimeo account, so an admin can fill a
// course from a Vimeo folder: each video becomes a lesson with its title,
// length, player link and thumbnail. It only reads; it never changes the
// account.
package vimeo

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// API is Vimeo's API.
const API = "https://api.vimeo.com"

// Client calls the Vimeo API with a personal access token (developer.vimeo.com,
// scopes public and private).
type Client struct {
	token string
	base  string
	http  *http.Client
}

// New returns a client for the token, or nil when token is "".
func New(token string) *Client {
	if token == "" {
		return nil
	}
	return &Client{token: token, base: API, http: &http.Client{Timeout: 30 * time.Second}}
}

// FromEnv returns a client for VIMEO_TOKEN, or nil when it isn't set.
func FromEnv() *Client { return New(strings.TrimSpace(os.Getenv("VIMEO_TOKEN"))) }

// WithBase points the client at another server, for tests.
func (c *Client) WithBase(base string) *Client { c.base = strings.TrimRight(base, "/"); return c }

// WithHTTP sets the HTTP client, for tests against a TLS test server.
func (c *Client) WithHTTP(hc *http.Client) *Client { c.http = hc; return c }

// Account is the signed-in Vimeo user and their plan.
type Account struct {
	Name string `json:"name"`
	Link string `json:"link"`
	Plan string `json:"account"` // basic (free), starter, standard, advanced, plus, pro, business, …
}

// Paid reports whether the plan can lock embeds to chosen domains. Free
// ("free" or the old "basic") can't.
func (a Account) Paid() bool { return a.Plan != "" && a.Plan != "basic" && a.Plan != "free" }

// Folder is a Vimeo folder ("project").
type Folder struct {
	User   string // the owner's user id
	ID     string
	Name   string
	Videos int
}

// URL is the folder's page on vimeo.com, the form ParseFolderURL takes.
func (f Folder) URL() string { return "https://vimeo.com/user/" + f.User + "/folder/" + f.ID }

// Video is one video in a folder.
type Video struct {
	ID      string
	Name    string
	Link    string    // vimeo.com page
	Embed   string    // player URL; carries ?h= for an unlisted video
	Seconds int       // length
	Thumb   string    // a thumbnail about 640 wide, or "" when Vimeo has none
	Created time.Time // upload time
	View    string    // privacy.view: anybody, unlisted, disable (hide from Vimeo), nobody, password, …
	EmbedOK string    // privacy.embed: public, private, whitelist
}

// Minutes is the video's length in whole minutes, rounded up.
func (v Video) Minutes() int64 { return int64((v.Seconds + 59) / 60) }

// Title is the video's title as a lesson's (LessonTitle).
func (v Video) Title() string { return LessonTitle(v.Name) }

// Contents is what a folder holds.
type Contents struct {
	Folders []Folder
	Videos  []Video
}

// Error is a refusal from Vimeo.
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return fmt.Sprintf("vimeo: %d %s", e.Status, e.Msg) }

// NotFound reports whether err is Vimeo saying there's no such thing (or the
// token can't see it).
func NotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && (e.Status == http.StatusNotFound || e.Status == http.StatusForbidden)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	u := path
	if !strings.HasPrefix(u, "https://") {
		u = c.base + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	// The token goes in a header, never the URL: a failed request's error
	// quotes its URL, and errors are logged.
	req.Header.Set("Authorization", "bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.vimeo.*+json;version=3.4")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return &Error{resp.StatusCode, e.Error}
	}
	return json.Unmarshal(body, out)
}

// Account reads the token's user and plan.
func (c *Client) Account(ctx context.Context) (Account, error) {
	var a Account
	err := c.get(ctx, "/me?fields=name,link,account", &a)
	return a, err
}

// page is one page of a Vimeo list.
type page[T any] struct {
	Data   []T `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

// all follows a list's pages, up to 20 of them.
func all[T any](ctx context.Context, c *Client, path string) ([]T, error) {
	var out []T
	for i := 0; path != "" && i < 20; i++ {
		var p page[T]
		if err := c.get(ctx, path, &p); err != nil {
			return nil, err
		}
		out = append(out, p.Data...)
		path = p.Paging.Next
	}
	return out, nil
}

type apiFolder struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Metadata struct {
		Connections struct {
			Videos struct {
				Total int `json:"total"`
			} `json:"videos"`
		} `json:"connections"`
	} `json:"metadata"`
}

type apiVideo struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	Link     string `json:"link"`
	Embed    string `json:"player_embed_url"`
	Duration int    `json:"duration"`
	Created  string `json:"created_time"`
	Pictures struct {
		Type  string `json:"type"` // "default" is Vimeo's grey placeholder
		Sizes []struct {
			Width int    `json:"width"`
			Link  string `json:"link"`
		} `json:"sizes"`
	} `json:"pictures"`
	Privacy struct {
		View  string `json:"view"`
		Embed string `json:"embed"`
	} `json:"privacy"`
}

const folderFields = "uri,name,metadata.connections.videos.total"
const videoFields = "uri,name,link,player_embed_url,duration,created_time,pictures.type,pictures.sizes,privacy.view,privacy.embed"

// uriPart pulls the id after key in a Vimeo uri ("/users/1/projects/2").
func uriPart(uri, key string) string {
	parts := strings.Split(strings.Trim(uri, "/"), "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] == key {
			return parts[i+1]
		}
	}
	return ""
}

func (f apiFolder) folder(user string) Folder {
	if u := uriPart(f.URI, "users"); u != "" {
		user = u
	}
	return Folder{User: user, ID: uriPart(f.URI, "projects"), Name: f.Name, Videos: f.Metadata.Connections.Videos.Total}
}

func (v apiVideo) video() Video {
	out := Video{
		ID: uriPart(v.URI, "videos"), Name: strings.TrimSpace(v.Name), Link: v.Link, Embed: v.Embed,
		Seconds: v.Duration, View: v.Privacy.View, EmbedOK: v.Privacy.Embed,
	}
	if t, err := time.Parse(time.RFC3339, v.Created); err == nil {
		out.Created = t
	}
	// The smallest picture at least 640 wide, else the biggest. A video with
	// no thumbnail set gets Vimeo's grey camera placeholder: leave it out.
	if v.Pictures.Type == "default" {
		return out
	}
	bigEnough, biggest := -1, -1
	for _, s := range v.Pictures.Sizes {
		if s.Width >= 640 && (bigEnough < 0 || s.Width < bigEnough) {
			bigEnough, out.Thumb = s.Width, s.Link
		}
		if bigEnough < 0 && s.Width > biggest {
			biggest, out.Thumb = s.Width, s.Link
		}
	}
	return out
}

// MyFolders lists the token user's own top-level folders.
func (c *Client) MyFolders(ctx context.Context) ([]Folder, error) {
	rows, err := all[apiFolder](ctx, c, "/me/projects?per_page=100&fields="+folderFields)
	if err != nil {
		return nil, err
	}
	out := make([]Folder, 0, len(rows))
	for _, f := range rows {
		out = append(out, f.folder(""))
	}
	return out, nil
}

// Folder lists what's in a folder: its subfolders and its videos, in lesson
// order (SortLessons). It reads the folder's items, which include
// subfolders, and falls back to its videos where items isn't offered.
func (c *Client) Folder(ctx context.Context, user, id string) (Contents, error) {
	base := "/users/" + url.PathEscape(user) + "/projects/" + url.PathEscape(id)
	type item struct {
		Type   string    `json:"type"`
		Folder apiFolder `json:"folder"`
		Video  apiVideo  `json:"video"`
	}
	fields := "type,folder." + strings.ReplaceAll(folderFields, ",", ",folder.") + ",video." + strings.ReplaceAll(videoFields, ",", ",video.")
	var out Contents
	items, err := all[item](ctx, c, base+"/items?per_page=100&fields="+fields)
	if err == nil {
		for _, it := range items {
			switch it.Type {
			case "folder":
				out.Folders = append(out.Folders, it.Folder.folder(user))
			case "video":
				out.Videos = append(out.Videos, it.Video.video())
			}
		}
	} else {
		var e *Error
		if !errors.As(err, &e) || e.Status != http.StatusNotFound {
			return out, err
		}
		videos, err := all[apiVideo](ctx, c, base+"/videos?per_page=100&fields="+videoFields)
		if err != nil {
			return out, err
		}
		for _, v := range videos {
			out.Videos = append(out.Videos, v.video())
		}
	}
	SortLessons(out.Videos)
	slices.SortStableFunc(out.Folders, func(a, b Folder) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out, nil
}

// leadingNumber matches a title's lesson number and what follows it up to
// the title proper: "Lesson 3: ", "3. ", "Part 2 - ", or "Lesson 6" run into
// the next word ("Lesson 6Support Systems", as one of Consortium's is).
var leadingNumber = regexp.MustCompile(`^\s*(?:(?:lesson|part|module|episode|ep\.?)\s*)?(\d{1,3})(?:\s*[:.)\-\x{2013}\x{2014}]+)?\s*`)

// lessonNumber is the number a title starts with ("3. Diversification",
// "Lesson 3 - …"), or -1.
func lessonNumber(title string) int {
	m := leadingNumber.FindStringSubmatch(strings.ToLower(title))
	if m == nil || len(m[0]) < len(title) && isDigit(title[len(m[0])]) {
		return -1
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// invisible are the zero-width characters pasted titles carry.
var invisible = strings.NewReplacer("\u200b", "", "\u200c", "", "\u200d", "", "\ufeff", "", "\u00a0", " ")

// LessonTitle is a video's title as a lesson's: without its lesson number
// (the site numbers lessons itself) or zero-width characters, with runs of
// spaces closed up. "Lesson 3:  Understanding Tax Rules" becomes
// "Understanding Tax Rules".
func LessonTitle(name string) string {
	name = strings.Join(strings.Fields(invisible.Replace(name)), " ")
	if lessonNumber(name) < 0 {
		return name
	}
	m := leadingNumber.FindString(strings.ToLower(name))
	if rest := strings.TrimSpace(name[len(m):]); rest != "" {
		return rest
	}
	return name
}

// SortLessons puts a folder's videos in lesson order: by the number their
// titles start with when every title has one, otherwise by upload time.
func SortLessons(videos []Video) {
	numbered := len(videos) > 0
	for _, v := range videos {
		if lessonNumber(v.Name) < 0 {
			numbered = false
			break
		}
	}
	slices.SortStableFunc(videos, func(a, b Video) int {
		if numbered {
			if c := cmp.Compare(lessonNumber(a.Name), lessonNumber(b.Name)); c != 0 {
				return c
			}
		}
		return a.Created.Compare(b.Created)
	})
}

var folderPath = regexp.MustCompile(`^/user/(\d+)/folder/(\d+)/?$`)

// ParseFolderURL takes a folder's link as Vimeo's address bar shows it
// (https://vimeo.com/user/8765158/folder/26617132) and returns its owner and
// id. Share links (vimeo.com/share/folder/…) can't be read by the API.
func ParseFolderURL(raw string) (user, id string, ok bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "", false
	}
	if h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www."); h != "vimeo.com" {
		return "", "", false
	}
	m := folderPath.FindStringSubmatch(u.Path)
	if m == nil {
		return "", "", false
	}
	return m[1], m[2], true
}

// thumbHost is where Vimeo serves thumbnails; Thumbnail fetches nothing else.
const thumbHost = "i.vimeocdn.com"

// Thumbnail downloads a video's thumbnail (Video.Thumb), up to 2 MB.
func (c *Client) Thumbnail(ctx context.Context, link string) (contentType string, data []byte, err error) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" || (u.Hostname() != thumbHost && c.base == API) {
		return "", nil, fmt.Errorf("vimeo: not a thumbnail link")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return "", nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", nil, &Error{resp.StatusCode, "thumbnail"}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return "", nil, err
	}
	contentType = http.DetectContentType(data)
	if !strings.HasPrefix(contentType, "image/") {
		return "", nil, fmt.Errorf("vimeo: thumbnail is %s", contentType)
	}
	return contentType, data, nil
}
