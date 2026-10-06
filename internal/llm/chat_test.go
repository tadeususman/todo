package llm

import "testing"

func TestInterpretChat(t *testing.T) {
	// plain text (model ignored the format): shown as-is, no actions
	if r, a := interpretChat("Halo, ini jawaban biasa"); r != "Halo, ini jawaban biasa" || a != nil {
		t.Errorf("plain: %q %v", r, a)
	}
	// broken JSON never becomes an action
	if r, a := interpretChat(`{"reply": "x", "actions": [oops`); a != nil || r == "" {
		t.Errorf("broken: %q %v", r, a)
	}
	// fenced JSON with an action
	raw := "```json\n{\"reply\":\"Siap, kusiapkan.\",\"actions\":[{\"task_id\":12,\"field\":\"priority\",\"value\":\"medium\"}]}\n```"
	r, a := interpretChat(raw)
	if r != "Siap, kusiapkan." || len(a) != 1 || a[0].TaskID != 12 || a[0].Field != "priority" || a[0].Value != "medium" {
		t.Errorf("fenced: %q %+v", r, a)
	}
	// a title sent by the model is only a hint; Prepare overwrites it from the DB
	// empty reply but with actions gets a default sentence
	if r, a := interpretChat(`{"reply":"","actions":[{"task_id":1,"field":"status","value":"done"}]}`); r == "" || len(a) != 1 {
		t.Errorf("empty reply: %q %v", r, a)
	}
	// reply only
	if r, a := interpretChat(`{"reply":"Cuma saran","actions":[]}`); r != "Cuma saran" || len(a) != 0 {
		t.Errorf("reply only: %q %v", r, a)
	}
	// more than MaxActions are cut
	big := `{"reply":"x","actions":[`
	for i := 0; i < 30; i++ {
		if i > 0 {
			big += ","
		}
		big += `{"task_id":1,"field":"priority","value":"low"}`
	}
	big += `]}`
	if _, a := interpretChat(big); len(a) != 10 {
		t.Errorf("cap: %d", len(a))
	}
}
