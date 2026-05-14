package classifier

import "testing"

func TestNormalizeError(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// Millisecond patterns replaced before plain digits.
		{"Connection timeout after 5000ms", "connection timeout after <ms>"},
		// Plain digits replaced.
		{"Expected 42 but got 0", "expected <n> but got <n>"},
		// URLs replaced.
		{"See https://example.com/path/to/docs for details", "see <url> for details"},
		// Uppercase folded, surrounding whitespace trimmed.
		{"  ASSERTION FAILED  ", "assertion failed"},
		// Mixed: ms, digits, and URL together.
		{"GET https://api.example.com failed after 3 retries (2500ms)", "get <url> failed after <n> retries (<ms>)"},
		// Two messages differing only in numbers normalize identically —
		// this is the property that makes error diversity meaningful.
		{"error on line 42: null pointer", "error on line <n>: null pointer"},
		{"error on line 99: null pointer", "error on line <n>: null pointer"},
		// Empty string passes through cleanly.
		{"", ""},
	}

	for _, c := range cases {
		got := normalizeError(c.input)
		if got != c.want {
			t.Errorf("normalizeError(%q)\n  got  %q\n  want %q", c.input, got, c.want)
		}
	}
}
