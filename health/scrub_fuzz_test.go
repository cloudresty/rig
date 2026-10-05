package health

import (
	"strings"
	"testing"
)

// pwAlphabet is what FuzzScrubCredentialsNeverLeaksPassword draws password
// characters from: every delimiter the rules lean on, plus letters and digits.
var pwAlphabet = []string{
	"@", ":", "/", "?", "&", "=", "#", "%", "\"", "'", "<", ">", " ", "\t", ",", ";", "(", ")", "://",
	"a", "b", "Z", "9", "x", "q", "password=", "token=", "@@", "amqp://",
}

// pwFromBytes builds a password from the alphabet, so the fuzzer explores
// delimiter combinations rather than random bytes.
func pwFromBytes(b []byte, noSpace bool) string {
	var sb strings.Builder
	for _, c := range b {
		tok := pwAlphabet[int(c)%len(pwAlphabet)]
		if noSpace && strings.ContainsAny(tok, " \t\"'") {
			tok = "k"
		}
		sb.WriteString(tok)
	}
	return sb.String()
}

// isStructural reports a window that is part of text the scrubber keeps on
// purpose and that carries no secret: a credential parameter label ("password=")
// or a scheme prefix ("amqp://").
func isStructural(w string) bool {
	for _, k := range []string{"password=", "passwd=", "pass=", "pwd=", "secret=", "token=", "apikey=", "api_key=", "amqp://"} {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

func FuzzScrubCredentialsNeverLeaksPassword(f *testing.F) {
	idx := func(tok string) int {
		for i, a := range pwAlphabet {
			if a == tok {
				return i
			}
		}
		panic(tok)
	}
	enc := func(toks ...string) []byte {
		b := make([]byte, len(toks))
		for i, t := range toks {
			b[i] = byte(idx(t))
		}
		return b
	}
	// seeds: the five leaks verified against v1.12.0, as alphabet encodings.
	seeds := [][]byte{
		enc("a", ":", "a", "@", "x", "/", "b", " ", "b", "\"", "'", "x", "://", "a"),
		enc("a", "@", "x", ":", "x", "\"", "q", "@", "q"),
		enc("q", "&", "password=", "Z", "Z"),
		enc("a", ":", "b", "@", "x"),
		enc("b", "://", "Z", "Z"),
	}
	for tmpl := uint8(0); tmpl < 8; tmpl++ {
		for _, sd := range seeds {
			f.Add(tmpl, "dial ", " end", sd)
		}
	}
	f.Add(uint8(0), "", "", []byte{})

	f.Fuzz(func(t *testing.T, tmpl uint8, prefix, suffix string, raw []byte) {
		if len(raw) > 24 {
			raw = raw[:24]
		}
		const marker = "XPWX" // placeholder swapped for the password below
		var body string
		schemeless := false
		switch tmpl % 8 {
		case 0:
			body = "amqp://user:" + marker + "@broker.internal:5672/vh"
		case 1:
			body = "mongodb+srv://u:" + marker + "@h1.internal/db amqps://v:" + marker + "@h2.internal:5671/"
		case 2:
			body = "http://x:" + marker + "@h.internal/p?password=" + marker + "&x=1"
		case 3:
			body = "u:" + marker + "@host.internal:5432"
			schemeless = true
		case 4:
			body = "dial amqp://u:" + marker + "@broker.internal:5672: timeout"
		case 5:
			body = "?password=" + marker + "&x=1"
		case 6:
			body = "redis://:" + marker + "@cache.internal:6379/0 and amqp://a:" + marker + "@b.internal"
		case 7:
			body = "svc: root:" + marker + "@db.internal:5432 refused"
			schemeless = true
		}
		// Scheme-less tokens are delimited by whitespace, so a password with
		// whitespace or a quote there cannot be told from surrounding text;
		// that is the one documented limit, excluded from the property.
		pw := pwFromBytes(raw, schemeless)
		if pw == "" {
			return
		}
		in := prefix + strings.ReplaceAll(body, marker, pw) + suffix
		base := prefix + strings.ReplaceAll(body, marker, "") + suffix
		out := ScrubCredentials(in)
		// the marker itself is not password text ("://<" can straddle it)
		plain := strings.ReplaceAll(out, redacted, "\x00")
		for i := 0; i+4 <= len(pw); i++ {
			w := pw[i : i+4]
			if strings.Contains(base, w) || isStructural(w) {
				continue
			}
			if strings.Contains(plain, w) {
				t.Fatalf("leaked %q of password %q\n in: %q\nout: %q", w, pw, in, out)
			}
		}
		if again := ScrubCredentials(out); again != out {
			t.Fatalf("not idempotent\n in: %q\nout: %q\n 2x: %q", in, out, again)
		}
	})
}
