// Package llm: adapter untuk 2 provider: claude via bridge (primary) + gemini flash (fallback).
package llm

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	ProviderBridge = "bridge"
	ProviderGemini = "gemini"
)

type Config struct {
	Provider    string
	BridgeURL   string
	BridgeModel string
	GeminiKey   string
	GeminiModel string
}

type Client struct {
	cfg  Config
	http *http.Client
	db   *sql.DB
}

func New(cfg Config, db *sql.DB) *Client {
	return &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: 60 * time.Second},
		db:   db,
	}
}

func (c *Client) Provider() string { return c.cfg.Provider }
func (c *Client) Model() string {
	if c.cfg.Provider == ProviderGemini {
		return c.cfg.GeminiModel
	}
	return c.cfg.BridgeModel
}

// send dispatches to the configured provider. Returns raw text output.
func (c *Client) send(ctx context.Context, system, user string) (output string, provider, model string, err error) {
	if c.cfg.Provider == ProviderGemini {
		out, err := c.sendGemini(ctx, system, user)
		return out, ProviderGemini, c.cfg.GeminiModel, err
	}
	out, err := c.sendBridge(ctx, system, user)
	if err != nil && c.cfg.GeminiKey != "" {
		// bridge failed → fallback to gemini
		out, gerr := c.sendGemini(ctx, system, user)
		if gerr == nil {
			return out, ProviderGemini, c.cfg.GeminiModel, nil
		}
	}
	return out, ProviderBridge, c.cfg.BridgeModel, err
}

// ---- bridge ----

type bridgeRequest struct {
	Prompt string `json:"prompt"`
	App    string `json:"app"`
}

type bridgeResponse struct {
	Output string `json:"output"`
	Error  string `json:"error"`
}

func (c *Client) sendBridge(ctx context.Context, system, user string) (string, error) {
	var sb strings.Builder
	if system != "" {
		sb.WriteString(system)
		sb.WriteString("\n\n")
	}
	sb.WriteString("User: ")
	sb.WriteString(user)

	body, err := json.Marshal(bridgeRequest{Prompt: sb.String(), App: "todo"})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BridgeURL+"/ask", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("bridge: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed bridgeResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("bridge response: %s", raw)
	}
	if parsed.Error != "" {
		return "", errors.New(parsed.Error)
	}
	return extractBridgeOutput(parsed.Output), nil
}

func extractBridgeOutput(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, `{"type": "tool_use"`) {
		return raw
	}
	var block struct {
		Input struct {
			Content string `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal([]byte(trimmed), &block); err == nil && block.Input.Content != "" {
		return block.Input.Content
	}
	return raw
}

// ---- gemini ----

func (c *Client) sendGemini(ctx context.Context, system, user string) (string, error) {
	if c.cfg.GeminiKey == "" {
		return "", errors.New("gemini: GEMINI_API_KEY not set")
	}
	type part struct {
		Text string `json:"text"`
	}
	type content struct {
		Parts []part `json:"parts"`
	}
	payload := map[string]any{
		"contents": []content{{Parts: []part{{Text: user}}}},
		"generationConfig": map[string]any{
			"temperature":     0.3,
			"maxOutputTokens": 1024,
		},
	}
	if system != "" {
		payload["systemInstruction"] = content{Parts: []part{{Text: system}}}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		c.cfg.GeminiModel, c.cfg.GeminiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("gemini response: %s", raw)
	}
	if parsed.Error != nil {
		return "", errors.New(parsed.Error.Message)
	}
	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", errors.New("gemini: empty response")
	}
	return parsed.Candidates[0].Content.Parts[0].Text, nil
}

// logCall writes an audit row — best-effort, never blocks the caller.
func (c *Client) logCall(action, input, output, provider, model string, errMsg string, dur time.Duration) {
	if c.db == nil {
		return
	}
	_, _ = c.db.Exec(`
		INSERT INTO ai_logs (provider, model, action, input, output, error_msg, duration_ms)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6,''), $7)`,
		provider, model, action, input, output, errMsg, dur.Milliseconds())
}
