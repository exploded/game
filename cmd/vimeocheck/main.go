// Command vimeocheck reports what the Vimeo token can see: the account's
// plan (which decides whether course videos can stay on Vimeo, locked to
// our domain), its folders, and for each folder link given, the videos and
// their privacy. It only reads. It is never deployed.
//
//	VIMEO_TOKEN=… go run ./cmd/vimeocheck https://vimeo.com/user/8765158/folder/26617132
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/exploded/game/internal/vimeo"
)

func main() {
	c := vimeo.FromEnv()
	if c == nil {
		fmt.Fprintln(os.Stderr, "Set VIMEO_TOKEN first.")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	a, err := c.Account(ctx)
	if err != nil {
		fail(err)
	}
	fmt.Printf("Account: %s (%s)\nPlan: %s", a.Name, a.Link, a.Plan)
	if a.Paid() {
		fmt.Println(" (paid: embeds can be locked to our domain)")
	} else {
		fmt.Println(" (free: no domain lock, Vimeo branding on embeds)")
	}

	mine, err := c.MyFolders(ctx)
	if err != nil {
		fmt.Println("Own folders: can't list:", err)
	} else {
		fmt.Printf("\nOwn folders (%d):\n", len(mine))
		for _, f := range mine {
			fmt.Printf("  %-50s %3d videos  %s\n", f.Name, f.Videos, f.URL())
		}
	}

	for _, link := range os.Args[1:] {
		user, id, ok := vimeo.ParseFolderURL(link)
		if !ok {
			fmt.Printf("\n%s: not a folder link (open the folder in Vimeo and copy the address bar)\n", link)
			continue
		}
		show(ctx, c, user, id, link, 0)
	}
}

// show prints a folder's videos, then its subfolders one level down.
func show(ctx context.Context, c *vimeo.Client, user, id, name string, depth int) {
	pad := strings.Repeat("  ", depth)
	got, err := c.Folder(ctx, user, id)
	if err != nil {
		fmt.Printf("\n%s%s: %v\n", pad, name, err)
		return
	}
	fmt.Printf("\n%s%s: %d videos, %d folders\n", pad, name, len(got.Videos), len(got.Folders))
	total := 0
	for _, v := range got.Videos {
		total += v.Seconds
		hash := ""
		if strings.Contains(v.Embed, "h=") {
			hash = " hash"
		}
		fmt.Printf("%s  %-60s %3d min  view=%s embed=%s%s\n", pad, v.Name, v.Minutes(), v.View, v.EmbedOK, hash)
	}
	if len(got.Videos) > 0 {
		fmt.Printf("%s  total %d min\n", pad, (total+59)/60)
	}
	if depth > 0 {
		return
	}
	for _, f := range got.Folders {
		show(ctx, c, f.User, f.ID, f.Name+" ("+f.URL()+")", depth+1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
