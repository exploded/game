// Package mail renders StockGame's emails, queues them in the email_outbox
// table and sends them through Amazon SES. docs/emails.md lists every email.
package mail

// Category groups emails so people can turn them off together.
type Category string

const (
	Account   Category = "account"   // sign-in, welcome; can't be turned off
	Trades    Category = "trades"    // orders filled or expired, dividends, splits
	Games     Category = "games"     // game started, ending soon, results
	Digest    Category = "digest"    // the weekly summary
	Community Category = "community" // answers to your questions, new courses
	News      Category = "news"      // marketing from Rob; opt-in only
)

// CategoryInfo describes a category on the Settings page.
type CategoryInfo struct {
	Key         Category
	Label       string
	Description string
	Default     bool // used when the user has no email_prefs row
}

// Optional lists the categories people can turn off, in Settings order.
var Optional = []CategoryInfo{
	{Trades, "Trades", "When an order fills or expires, and dividends or splits on stocks you hold.", true},
	{Games, "Games", "When a game you're in starts, is about to end, and the final results.", true},
	{Digest, "Weekly summary", "A Monday email with how each of your games went last week.", true},
	{Community, "Community", "Answers to your questions and new courses.", true},
	{News, "News from Rob", "Tips, courses and announcements. Sent only if you opt in.", false},
}

// Info returns the category's description, and false for Account or an
// unknown key.
func Info(c Category) (CategoryInfo, bool) {
	for _, info := range Optional {
		if info.Key == c {
			return info, true
		}
	}
	return CategoryInfo{}, false
}
