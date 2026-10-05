package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"todo/internal/task"
)

const parseSystemPrompt = `Kamu adalah parser task-manager untuk aplikasi todo pribadi.
User akan memberi deskripsi task bebas dalam Bahasa Indonesia (bisa campur istilah teknis/Inggris).
Ekstrak informasi menjadi JSON dengan field berikut:

{
  "title": "ringkas, maks 80 karakter, imperative form",
  "description": "detail tambahan kalau ada, kalau tidak kosongkan string",
  "priority": "low | medium | high | urgent",
  "complexity": "simple | medium | complex",
  "deadline": "ISO-8601 dengan timezone +07:00 Asia/Jakarta, atau null kalau tidak disebut",
  "estimated_minutes": integer menit perkiraan, atau null,
  "project": "nama project dari daftar yang disediakan user, atau empty string kalau tidak jelas"
}

Panduan:
- "priority" default medium. Angkat ke high/urgent kalau user eksplisit bilang "urgent", "penting banget", "ASAP", "deadline hari ini", dll.
- "complexity": simple = <30 menit, medium = 30-120 menit, complex = >2 jam atau butuh breakdown.
- "deadline": kalau user bilang "besok jam 10" → besok 10:00 waktu Jakarta. "nanti sore" → hari ini 16:00. "minggu depan" → senin depan 09:00.
- Kalau waktu tidak disebut, deadline null.
- "project": pilih SATU dari daftar project yang disediakan di konteks berdasarkan kata kunci di teks user (nama project, kata kunci terkait). Case-insensitive matching. Kalau tidak yakin atau tidak ada yang cocok, kembalikan empty string "" (sistem akan fallback ke project default).
- Output HARUS valid JSON. Tanpa markdown fence, tanpa komentar, tanpa teks lain di luar JSON.`

// ParseTask uses the LLM to extract structured task fields from freeform input.
// projectNames is the list of project names the user has (for AI auto-assignment).
func (c *Client) ParseTask(ctx context.Context, input string, projectNames []string) (*task.Parsed, error) {
	start := time.Now()

	loc, _ := time.LoadLocation("Asia/Jakarta")
	now := time.Now().In(loc)
	projectCtx := "(tidak ada project)"
	if len(projectNames) > 0 {
		projectCtx = strings.Join(projectNames, ", ")
	}
	userPrompt := fmt.Sprintf("Waktu sekarang: %s (Asia/Jakarta, %s).\nDaftar project user: %s\n\nTask: %s",
		now.Format("Monday, 2 January 2006 15:04"), now.Format("Mon"), projectCtx, input)

	raw, provider, model, err := c.send(ctx, parseSystemPrompt, userPrompt)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	defer c.logCall("parse", input, raw, provider, model, errMsg, time.Since(start))
	if err != nil {
		return nil, err
	}

	jsonStr := extractJSON(raw)
	if jsonStr == "" {
		return nil, errors.New("llm parse: tidak ada JSON di output")
	}

	var p task.Parsed
	if err := json.Unmarshal([]byte(jsonStr), &p); err != nil {
		return nil, fmt.Errorf("llm parse unmarshal: %w (raw: %s)", err, jsonStr)
	}
	if p.Title == "" {
		return nil, errors.New("llm parse: title kosong")
	}
	return &p, nil
}

// extractJSON finds the first { ... } block in raw text, stripping markdown fences.
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}
