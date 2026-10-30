package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"

	"github.com/exploded/game/internal/db"
)

// Topic is one Learn topic as the classifier sees it.
type Topic struct {
	Slug, Name, Description string
}

// Classification is what the classifier decides about a video or question.
type Classification struct {
	Topics  []string `json:"topics"`  // topic slugs, best first
	Tickers []string `json:"tickers"` // stock codes it talks about, like "CBA" or "AAPL"
}

// Classifier sorts a video or question into topics.
type Classifier interface {
	Classify(ctx context.Context, topics []Topic, title, text string) (Classification, error)
}

// ErrRefused means the model declined to classify the text, so there's no
// point asking again.
var ErrRefused = errors.New("classifier refused")

// Claude classifies with the Claude API. It reads ANTHROPIC_API_KEY.
type Claude struct {
	client anthropic.Client
	model  string
}

// NewClaude returns a classifier using model, or claude-opus-5-5 when model
// is "".
func NewClaude(model string) *Claude {
	if model == "" {
		model = "claude-opus-5-5"
	}
	return &Claude{client: anthropic.NewClient(), model: model}
}

// Classify asks Claude which topics fit. Structured output keeps the answer
// to the topic slugs given, and low effort is plenty for sorting.
func (c *Claude) Classify(ctx context.Context, topics []Topic, title, text string) (Classification, error) {
	slugs := make([]string, len(topics))
	var list strings.Builder
	for i, t := range topics {
		slugs[i] = t.Slug
		fmt.Fprintf(&list, "- %s (%s): %s\n", t.Slug, t.Name, t.Description)
	}
	system := "You sort videos and questions from Rob Goudie, an Australian financial adviser, " +
		"into topics on a personal finance website for Australians.\n\n" +
		"Topics:\n" + list.String() + "\n" +
		"Pick the 1 to 3 topics the item is mainly about, best fit first. Pick none only if no topic " +
		"fits at all (for example a channel announcement). Ignore boilerplate such as disclaimers, " +
		"contact details and links to other videos.\n" +
		"Also list the stock or ETF codes the item discusses by name (ASX or US codes, like CBA, VAS " +
		"or AAPL). Leave the list empty if none are discussed."
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"topics": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "enum": slugs},
			},
			"tickers": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string"},
			},
		},
		"required":             []string{"topics", "tickers"},
		"additionalProperties": false,
	}

	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	resp, err := c.client.Beta.Messages.New(ctx, anthropic.BetaMessageNewParams{
		Model:     anthropic.Model(c.model),
		MaxTokens: 2000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock("Title: " + title + "\n\n" + text)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{
			Effort: anthropic.BetaOutputConfigEffortLow,
			Format: anthropic.BetaJSONOutputFormatParam{Schema: schema},
		},
		// A refusal on one model is retried on another, chosen by the API.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	})
	if err != nil {
		return Classification{}, err
	}
	if resp.StopReason == anthropic.BetaStopReasonRefusal {
		return Classification{}, ErrRefused
	}
	for _, block := range resp.Content {
		if tb, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
			var out Classification
			if err := json.Unmarshal([]byte(tb.Text), &out); err != nil {
				return Classification{}, fmt.Errorf("classifier answer %q: %w", tb.Text, err)
			}
			// Keep only known slugs, in case a model ignores the enum.
			out.Topics = slices.DeleteFunc(out.Topics, func(s string) bool { return !slices.Contains(slugs, s) })
			if len(out.Topics) > 3 {
				out.Topics = out.Topics[:3]
			}
			return out, nil
		}
	}
	return Classification{}, fmt.Errorf("classifier gave no text (stop reason %s)", resp.StopReason)
}

// classifyBatch is how many videos one run classifies. The scheduler starts
// a run each minute, so a 1,600-video backlog clears in under two hours.
const classifyBatch = 20

// classifyPending sorts videos Claude hasn't seen yet.
func (s *Service) classifyPending(ctx context.Context) error {
	videos, err := s.q.ListUnclassifiedVideos(ctx, classifyBatch)
	if err != nil || len(videos) == 0 {
		return err
	}
	topics, err := s.topics(ctx)
	if err != nil {
		return err
	}
	for _, v := range videos {
		if err := s.classifyVideo(ctx, topics, v); err != nil {
			return err
		}
	}
	return nil
}

// ClassifyVideo sorts one video now (the admin's "Reclassify").
func (s *Service) ClassifyVideo(ctx context.Context, id int64) error {
	if s == nil || s.classify == nil {
		return errors.New("set ANTHROPIC_API_KEY to sort videos")
	}
	v, err := s.q.GetVideo(ctx, id)
	if err != nil {
		return err
	}
	topics, err := s.topics(ctx)
	if err != nil {
		return err
	}
	return s.classifyVideo(ctx, topics, v)
}

func (s *Service) classifyVideo(ctx context.Context, topics []Topic, v db.Video) error {
	c, err := s.classify.Classify(ctx, topics, v.Title, v.Description)
	if errors.Is(err, ErrRefused) {
		slog.Warn("classifier refused a video", "video", v.ID, "title", v.Title)
		return s.q.SetVideoClassified(ctx, v.ID)
	}
	if err != nil {
		return fmt.Errorf("classify video %d: %w", v.ID, err)
	}
	if err := s.q.ClearClaudeVideoTopics(ctx, v.ID); err != nil {
		return err
	}
	for _, slug := range c.Topics {
		t, err := s.q.GetTopicBySlug(ctx, slug)
		if err != nil {
			continue
		}
		if err := s.q.AddVideoTopic(ctx, db.AddVideoTopicParams{VideoID: v.ID, TopicID: t.ID, Source: "claude"}); err != nil {
			return err
		}
	}
	if err := s.q.ClearVideoStocks(ctx, v.ID); err != nil {
		return err
	}
	for _, sym := range c.Tickers {
		sym = strings.ToUpper(strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(sym, ".AX"), ":ASX")))
		if sym == "" || len(sym) > 10 {
			continue
		}
		if err := s.q.AddVideoStockBySymbol(ctx, db.AddVideoStockBySymbolParams{VideoID: v.ID, Symbol: sym}); err != nil {
			return err
		}
	}
	return s.q.SetVideoClassified(ctx, v.ID)
}

// SuggestQuestionTopic picks a topic for a question asked without one. It
// runs in the background and does nothing without a classifier.
func (s *Service) SuggestQuestionTopic(id int64, title, body string) {
	if s == nil || s.classify == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		topics, err := s.topics(ctx)
		if err != nil {
			slog.Error("question topic", "error", err)
			return
		}
		c, err := s.classify.Classify(ctx, topics, title, body)
		if err != nil || len(c.Topics) == 0 {
			if err != nil && !errors.Is(err, ErrRefused) {
				slog.Error("question topic", "question", id, "error", err)
			}
			return
		}
		t, err := s.q.GetTopicBySlug(ctx, c.Topics[0])
		if err != nil {
			return
		}
		if err := s.q.SetQuestionTopic(ctx, db.SetQuestionTopicParams{TopicID: nullInt(t.ID), ID: id}); err != nil {
			slog.Error("set question topic", "question", id, "error", err)
		}
	}()
}

func (s *Service) topics(ctx context.Context) ([]Topic, error) {
	rows, err := s.q.ListTopics(ctx)
	if err != nil {
		return nil, err
	}
	topics := make([]Topic, len(rows))
	for i, t := range rows {
		topics[i] = Topic{Slug: t.Slug, Name: t.Name, Description: t.Description}
	}
	return topics, nil
}
