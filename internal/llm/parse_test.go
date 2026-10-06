package llm

import (
	"errors"
	"testing"
)

func TestInterpretParse(t *testing.T) {
	// task biasa (is_task absen → dianggap task, kompatibel dengan output lama)
	p, err := interpretParse(`{"title":"Call client","priority":"high"}`)
	if err != nil || p.Title != "Call client" {
		t.Fatalf("task biasa: %v %+v", err, p)
	}
	// is_task true eksplisit, dibungkus fence markdown
	if _, err := interpretParse("```json\n{\"is_task\":true,\"title\":\"Bayar listrik\"}\n```"); err != nil {
		t.Fatalf("is_task true: %v", err)
	}
	// bukan task → NotTaskError dengan pesan dari LLM
	_, err = interpretParse(`{"is_task":false,"reject_reason":"Maaf, hanya task ya."}`)
	var nt *NotTaskError
	if !errors.As(err, &nt) || nt.Message != "Maaf, hanya task ya." {
		t.Fatalf("bukan task: %v", err)
	}
	// bukan task tanpa alasan → pesan default
	_, err = interpretParse(`{"is_task":false}`)
	if !errors.As(err, &nt) || nt.Message != defaultNotTaskMsg {
		t.Fatalf("default msg: %v", err)
	}
	// alasan terlalu panjang → pesan default (cegah LLM menjawab panjang lebar)
	long := make([]byte, 400)
	for i := range long {
		long[i] = 'a'
	}
	_, err = interpretParse(`{"is_task":false,"reject_reason":"` + string(long) + `"}`)
	if !errors.As(err, &nt) || nt.Message != defaultNotTaskMsg {
		t.Fatalf("long reason: %v", err)
	}
	// title kosong & is_task tidak false → error biasa (fallback di handler)
	if _, err := interpretParse(`{"title":""}`); err == nil || errors.As(err, &nt) {
		t.Fatalf("title kosong harus error biasa: %v", err)
	}
}
