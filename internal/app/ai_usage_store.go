package app

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"carelockconsulting/internal/db"
)

type aiUsageStore interface {
	LogRequest(provider, model string, inputTokens, outputTokens int64) error
	GetSummary() (aiUsageSummary, error)
}

type aiUsageSummary struct {
	Providers []aiProviderUsage `json:"providers"`
	Total     aiProviderUsage   `json:"total"`
}

type aiProviderUsage struct {
	Provider          string `json:"provider,omitempty"`
	RequestCount      int64  `json:"request_count"`
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	TodayRequestCount int64  `json:"today_request_count"`
	TodayInputTokens  int64  `json:"today_input_tokens"`
	TodayOutputTokens int64  `json:"today_output_tokens"`
}

var activeAIUsageStore aiUsageStore = &memoryAIUsageStore{}

func configureAIUsageStore(conn *db.Conn) {
	if conn == nil {
		activeAIUsageStore = &memoryAIUsageStore{}
		return
	}
	activeAIUsageStore = &dbAIUsageStore{db: conn}
}

// memoryAIUsageStore is the fallback store used when no DB is configured
// (e.g. LOCAL_MODE without a SQLite path). Stats reset on restart.
type memoryAIUsageStore struct {
	mu  sync.Mutex
	log []memoryUsageRow
}

type memoryUsageRow struct {
	Provider     string
	Model        string
	InputTokens  int64
	OutputTokens int64
	CreatedAt    time.Time
}

func (s *memoryAIUsageStore) LogRequest(provider, model string, inputTokens, outputTokens int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, memoryUsageRow{
		Provider:     provider,
		Model:        model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		CreatedAt:    time.Now().UTC(),
	})
	return nil
}

func (s *memoryAIUsageStore) GetSummary() (aiUsageSummary, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	today := time.Now().UTC().Format("2006-01-02")
	byProvider := map[string]*aiProviderUsage{}

	for _, row := range s.log {
		p := byProvider[row.Provider]
		if p == nil {
			p = &aiProviderUsage{Provider: row.Provider}
			byProvider[row.Provider] = p
		}
		p.RequestCount++
		p.InputTokens += row.InputTokens
		p.OutputTokens += row.OutputTokens
		if row.CreatedAt.Format("2006-01-02") == today {
			p.TodayRequestCount++
			p.TodayInputTokens += row.InputTokens
			p.TodayOutputTokens += row.OutputTokens
		}
	}

	return buildSummary(byProvider), nil
}

type dbAIUsageStore struct {
	db *db.Conn
}

func (s *dbAIUsageStore) LogRequest(provider, model string, inputTokens, outputTokens int64) error {
	_, err := s.db.Exec(
		`INSERT INTO ai_usage_log (provider, model, input_tokens, output_tokens, created_at) VALUES (?, ?, ?, ?, ?)`,
		provider, model, inputTokens, outputTokens,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *dbAIUsageStore) GetSummary() (aiUsageSummary, error) {
	today := time.Now().UTC().Format("2006-01-02")

	rows, err := s.db.Query(`
		SELECT
			provider,
			COUNT(*),
			COALESCE(SUM(input_tokens), 0),
			COALESCE(SUM(output_tokens), 0),
			COUNT(CASE WHEN created_at >= ? THEN 1 END),
			COALESCE(SUM(CASE WHEN created_at >= ? THEN input_tokens END), 0),
			COALESCE(SUM(CASE WHEN created_at >= ? THEN output_tokens END), 0)
		FROM ai_usage_log
		GROUP BY provider
		ORDER BY provider`, today, today, today)
	if err != nil {
		return aiUsageSummary{}, err
	}
	defer rows.Close()

	byProvider := map[string]*aiProviderUsage{}
	for rows.Next() {
		var p aiProviderUsage
		if err := rows.Scan(
			&p.Provider,
			&p.RequestCount, &p.InputTokens, &p.OutputTokens,
			&p.TodayRequestCount, &p.TodayInputTokens, &p.TodayOutputTokens,
		); err != nil {
			return aiUsageSummary{}, err
		}
		byProvider[p.Provider] = &p
	}
	if err := rows.Err(); err != nil {
		return aiUsageSummary{}, err
	}

	return buildSummary(byProvider), nil
}

func buildSummary(byProvider map[string]*aiProviderUsage) aiUsageSummary {
	order := []string{"claude", "wintermute"}
	providers := make([]aiProviderUsage, 0, len(byProvider))
	seen := map[string]bool{}
	for _, name := range order {
		if p, ok := byProvider[name]; ok {
			providers = append(providers, *p)
			seen[name] = true
		}
	}
	for name, p := range byProvider {
		if !seen[name] {
			providers = append(providers, *p)
		}
	}

	var total aiProviderUsage
	for _, p := range providers {
		total.RequestCount += p.RequestCount
		total.InputTokens += p.InputTokens
		total.OutputTokens += p.OutputTokens
		total.TodayRequestCount += p.TodayRequestCount
		total.TodayInputTokens += p.TodayInputTokens
		total.TodayOutputTokens += p.TodayOutputTokens
	}

	return aiUsageSummary{Providers: providers, Total: total}
}

func aiChatUsageHandler(c *gin.Context) {
	summary, err := activeAIUsageStore.GetSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load usage"})
		return
	}
	// Ensure Providers is never null in JSON.
	if summary.Providers == nil {
		summary.Providers = []aiProviderUsage{}
	}
	data, err := json.Marshal(summary)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to encode usage"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

// logAIUsage fires-and-forgets a usage row; errors are silently dropped so
// a logging failure never breaks a successful AI response.
func logAIUsage(provider, model string, inputTokens, outputTokens int64) {
	_ = activeAIUsageStore.LogRequest(provider, model, inputTokens, outputTokens)
}
