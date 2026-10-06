package user

import "testing"

func TestValidateUsername(t *testing.T) {
	ok := []string{"tade", "Tadeus", "a.b-c_d", "user123", "abc"}
	bad := []string{"", "ab", "has space", "-start", "a/b", "waaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaay-too-long"}
	for _, s := range ok {
		if err := ValidateUsername(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateUsername(s); err == nil {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestNormalizeAndPassword(t *testing.T) {
	if got := NormalizeUsername("  TaDe "); got != "TaDe" {
		t.Errorf("got %q", got)
	}
	if ValidatePassword("short") == nil {
		t.Error("short password accepted")
	}
	if ValidatePassword("12345678") != nil {
		t.Error("8-char password rejected")
	}
	long := make([]byte, 73)
	for i := range long {
		long[i] = 'a'
	}
	if ValidatePassword(string(long)) == nil {
		t.Error(">72 bytes accepted")
	}
}

func TestValidateEmail(t *testing.T) {
	ok := []string{"a@b.co", "tade.us+x@mail.example.com"}
	bad := []string{"", "plain", "a@b", "Nama <a@b.co>", "a@@b.co", "a b@c.co"}
	for _, s := range ok {
		if err := ValidateEmail(s); err != nil {
			t.Errorf("%q should be valid: %v", s, err)
		}
	}
	for _, s := range bad {
		if err := ValidateEmail(s); err == nil {
			t.Errorf("%q should be invalid", s)
		}
	}
	if got := NormalizeEmail("  Tade@Mail.COM "); got != "tade@mail.com" {
		t.Errorf("got %q", got)
	}
}
