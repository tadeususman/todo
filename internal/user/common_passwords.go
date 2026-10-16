package user

// commonPasswords: sample of the most-used passwords that still pass the length check (8+ chars).
// Not exhaustive; it only blocks the obvious ones.
var commonPasswords = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range []string{
		"password", "password1", "password12", "password123", "passw0rd", "p@ssw0rd", "p@ssword",
		"12345678", "123456789", "1234567890", "123123123", "11111111", "00000000", "87654321",
		"qwertyui", "qwerty123", "qwertyuiop", "1q2w3e4r", "1qaz2wsx", "abc12345", "abcd1234", "abcdefgh",
		"iloveyou", "admin123", "administrator", "welcome1", "welcome123", "letmein1", "changeme",
		"rahasia123", "sayangku", "bismillah", "indonesia", "indonesia123", "kucing123", "12341234",
		"todo1234", "todotodo", "asdfghjk", "asdf1234", "zxcvbnm1", "monkey123", "dragon123",
	} {
		m[p] = true
	}
	return m
}()
