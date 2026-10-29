# To do

Ideas and requests not yet built. Move an item to `FEATURES.md` once it ships.

## Requests from Rob (2026-10-29)

- [ ] **Review the whole game for improvements.** Rob asked for another look at
  StockGame to find anything that could be better.
- [ ] **"Ask a Question" section.** A permanent, site-wide section where members
  submit questions and vote on other members' questions. Rob suggests starting
  small and adding to it as ideas come in.
- [ ] **Courses and other content.** Pre-recorded video courses inside
  StockGame, as part of the community platform discussed with Rob.
  - Host the videos elsewhere (YouTube, Vimeo or Bunny Stream) and embed them.
    The Linode shouldn't serve video.
  - Embeds need a `frame-src` entry in the CSP (`internal/handler/security.go`).
- [ ] **Email sign-up without Google.** Let people register with an email
  address instead of connecting a Google account.
  - A magic-link (emailed sign-in link) avoids storing passwords. Send it
    through Amazon SES, as other apps do.
  - Link an email account to an existing Google account when the addresses
    match, so nobody ends up with two accounts.
