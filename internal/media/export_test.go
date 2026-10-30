package media

import (
	"time"

	"github.com/exploded/game/internal/db"
	"github.com/exploded/game/internal/mail"
)

// Hooks for the tests in media_test, which can't be in this package because
// they open the database through handler.

func NewTestService(q *db.Queries, m *mail.Mailer, yt *YouTube, tt *TikTok, c Classifier) *Service {
	return &Service{q: q, mailer: m, loc: time.UTC, yt: yt, tt: tt, classify: c}
}

func (y *YouTube) UseTestServer(api, shorts string) { y.base, y.shorts = api, shorts }
func (t *TikTok) UseTestServer(api string)          { t.api = api }

var (
	SyncYouTube      = (*Service).syncYouTube
	CheckYouTubeLive = (*Service).checkYouTubeLive
	ClassifyPending  = (*Service).classifyPending
	NotifyLive       = (*Service).notifyLive
	MailOnAirAnswers = (*Service).mailOnAirAnswers
	SyncTikTok       = (*Service).syncTikTok
	ParseISODuration = parseISODuration
	StateBackfilled  = stateYouTubeBackfilled
)
