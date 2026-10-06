package chat

import "testing"

func TestNormalize(t *testing.T) {
	ok := map[[2]string]string{
		{"priority", " MEDIUM "}:                  "medium",
		{"status", "done"}:                        "done",
		{"deadline", ""}:                          "",
		{"deadline", "2026-10-08T10:00"}:          "2026-10-08T10:00",
		{"deadline", "2026-10-08 10:00"}:          "2026-10-08T10:00",
		{"deadline", "2026-10-08T10:00:00+07:00"}: "2026-10-08T10:00",
		{"deadline", "2026-10-08T03:00:00Z"}:      "2026-10-08T10:00", // converted to WIB
		{"title", "  Revisi laporan "}:            "Revisi laporan",
		{"project", "ERP Unirama"}:                "ERP Unirama",
	}
	for in, want := range ok {
		if got, good := normalize(in[0], in[1]); !good || got != want {
			t.Errorf("normalize(%q,%q) = %q,%v want %q", in[0], in[1], got, good, want)
		}
	}
	bad := [][2]string{
		{"priority", "super"}, {"priority", ""}, {"status", "selesai"}, {"deadline", "besok"},
		{"title", ""}, {"project", ""}, {"is_admin", "true"}, {"", "x"},
	}
	for _, in := range bad {
		if got, good := normalize(in[0], in[1]); good {
			t.Errorf("normalize(%q,%q) accepted as %q", in[0], in[1], got)
		}
	}
	long := make([]rune, maxTitleRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	if _, good := normalize("title", string(long)); good {
		t.Error("over-long title accepted")
	}
}

func TestLabel(t *testing.T) {
	cases := map[Action]string{
		{Field: "priority", Value: "medium"}:           "Priority → medium",
		{Field: "status", Value: "done"}:               "Status → selesai",
		{Field: "deadline", Value: ""}:                 "Deadline → dihapus",
		{Field: "deadline", Value: "2026-10-08T10:00"}: "Deadline → 8 Oct 2026 10:00",
		{Field: "project", Value: "General"}:           "Project → General",
	}
	for a, want := range cases {
		if got := a.Label(); got != want {
			t.Errorf("Label(%+v) = %q, want %q", a, got, want)
		}
	}
}
