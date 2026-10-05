package health

import (
	"regexp"
	"strings"
)

// redacted replaces whatever may be a credential. It is also what a custom
// scrubber that panics is replaced with, so a failure never leaks the input.
const redacted = "<redacted>"

// paramRe matches a credential parameter name and its '='. The name is not
// word-bounded on purpose ("api_token=", "x-secret=" match): over-redaction is
// acceptable, a leak is not.
var paramRe = regexp.MustCompile(`(?i)(password|passwd|pass|pwd|secret|token|apikey|api_key)=`)

// ScrubCredentials returns s with credentials removed. It is what rig/health
// applies to every string it renders in /health/live, /health/ready and
// /health/startup, exported so a service can reuse the same rules for logs
// and client ping errors.
//
// A password is attacker-shaped text: it may contain '@', ':', '/', '&',
// quotes, whitespace, even "://" or "password=". The rules therefore never try
// to guess where a secret ends; they keep a host only when the shape is
// unambiguous and otherwise redact to the end of the string. In order:
//
//  1. Host kept, only when unambiguous: the string holds exactly one scheme://
//     whose scheme is a pure [A-Za-z][A-Za-z0-9+.-]* token at the start or after
//     whitespace, a quote, '=', '(' or ','; the whole string holds exactly one
//     '@', and it precedes the next whitespace or quote; the userinfo holds no
//     credential parameter name. The result is scheme://<redacted>@host...
//  2. Any other string with a scheme:// followed anywhere by '@' or a credential
//     parameter name keeps the scheme:// and loses EVERYTHING after it. When the
//     text just before "://" is not a pure scheme (user:pa://ss@host) the whole
//     token it belongs to is redacted instead.
//  3. A scheme-less user:password@host token (whitespace-delimited) becomes
//     <redacted>@host when it has one '@' and no quote inside the userinfo;
//     otherwise the token and everything after it is redacted. A password
//     containing whitespace or a quote cannot be told from surrounding text in
//     a scheme-less token and is the one documented limit.
//  4. password=, passwd=, pass=, pwd=, secret=, token=, apikey= and api_key=
//     (any case): the value is redacted to the END of the string, not to '&' or
//     a quote, because a hostile value can contain both. This runs last so it
//     cannot expose what the URI rules protect, and they cannot expose it.
//
// It over-redacts rather than risk a leak, is idempotent, and never panics.
func ScrubCredentials(s string) string {
	if s == "" {
		return s
	}
	s = scrubSchemeURI(s)
	s = scrubBareUserinfo(s)
	return scrubParams(s)
}

func scrubParams(s string) string {
	loc := paramRe.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[1]] + redacted
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func isWSQuote(b byte) bool { return isSpace(b) || b == '"' || b == '\'' }

func isSchemeChar(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '+' || b == '.' || b == '-'
}

func isAlpha(b byte) bool { return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' }

func scrubSchemeURI(s string) string {
	i := strings.Index(s, "://")
	if i < 0 {
		return s
	}
	rest := s[i+3:]
	if !strings.Contains(rest, "@") && !paramRe.MatchString(rest) {
		return s // a plain URL; any later "://" is inside rest and equally plain
	}
	j := i
	for j > 0 && isSchemeChar(s[j-1]) {
		j--
	}
	// The token the scheme sits in starts at k. It is a pure scheme only when
	// it begins the token or follows '=', '(' or ',' with no ':' or '@' ahead
	// of it in that token (those would be userinfo already: root:a=amqp://).
	k := j
	for k > 0 && !isWSQuote(s[k-1]) {
		k--
	}
	pure := j < i && isAlpha(s[j]) && (j == 0 || isWSQuote(s[j-1]) || strings.IndexByte("=(,", s[j-1]) >= 0) &&
		!strings.ContainsAny(s[k:j], ":@")
	if !pure {
		return s[:k] + redacted
	}
	if strings.Count(s, "://") == 1 && strings.Count(s, "@") == 1 {
		end := len(rest)
		for n := 0; n < len(rest); n++ {
			if isWSQuote(rest[n]) {
				end = n
				break
			}
		}
		seg := rest[:end]
		if strings.Count(seg, "@") == 1 {
			at := strings.IndexByte(rest, '@')
			if !paramRe.MatchString(rest[:at]) {
				return s[:i+3] + redacted + rest[at:]
			}
		}
	}
	return s[:i+3] + redacted
}

func scrubBareUserinfo(s string) string {
	if !strings.Contains(s, "@") {
		return s
	}
	var out strings.Builder
	i := 0
	for i < len(s) {
		j := i
		for j < len(s) && !isSpace(s[j]) {
			j++
		}
		r, all := scrubToken(s[i:j])
		out.WriteString(r)
		if all {
			return out.String()
		}
		k := j
		for k < len(s) && isSpace(s[k]) {
			k++
		}
		out.WriteString(s[j:k])
		i = k
	}
	return out.String()
}

// scrubToken returns the scrubbed token; all reports that the token and
// everything after it must be dropped (an ambiguous userinfo).
func scrubToken(tok string) (string, bool) {
	at := strings.LastIndexByte(tok, '@')
	if at < 0 || strings.Contains(tok, "://"+redacted+"@") {
		return tok, false
	}
	if loc := paramRe.FindStringIndex(tok); loc != nil {
		if strings.ContainsAny(tok[:loc[0]], ":@") {
			return redacted, true // userinfo ahead of a parameter label: root:pw&password=x@h
		}
		return tok, false // password=...: the parameter rule takes it to the end
	}
	prefix := tok[:at]
	if !strings.Contains(prefix, ":") {
		return tok, false
	}
	start, ambiguous := 0, false
	for q := 0; q < len(prefix); q++ {
		if prefix[q] != '"' && prefix[q] != '\'' {
			continue
		}
		if q == 0 || strings.IndexByte("=(,[{:", prefix[q-1]) >= 0 {
			start = q + 1
		} else {
			ambiguous = true
		}
	}
	if ambiguous || strings.Count(tok, "@") > 1 {
		return redacted, true
	}
	if !strings.Contains(prefix[start:], ":") {
		return tok, false
	}
	return tok[:start] + redacted + tok[at:], false
}

// WithScrubber adds an extra scrubber, applied to every rendered string AFTER
// ScrubCredentials, for patterns the built-in rules cannot know (an internal
// token format, say). Several may be given and run in order. The built-in
// scrubber cannot be disabled or replaced. A scrubber that panics has its
// input replaced by "<redacted>"; a nil f is ignored.
func WithScrubber(f func(string) string) RegistryOption {
	return func(r *Registry) {
		if f != nil {
			r.extraScrub = append(r.extraScrub, f)
		}
	}
}

// scrub is the single choke point every rendered string goes through.
func (r *Registry) scrub(s string) string {
	s = ScrubCredentials(s)
	for _, f := range r.extraScrub {
		s = applyScrubber(f, s)
	}
	return s
}

func applyScrubber(f func(string) string, s string) (out string) {
	defer func() {
		if recover() != nil {
			out = redacted
		}
	}()
	return f(s)
}
