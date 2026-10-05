package health

import (
	"regexp"
	"strings"
)

// redacted replaces whatever may be a credential. It is also what a custom
// scrubber that panics is replaced with, so a failure never leaks the input.
const redacted = "<redacted>"

var (
	// paramRe matches credential-looking key=value pairs. The value runs to
	// the next '&', quote, newline or the end of the string; it may contain
	// ':', '@', '/' and spaces. The key is not word-bounded on purpose
	// ("api_token=", "x-secret=" match): over-redaction is acceptable, a leak
	// is not.
	paramRe = regexp.MustCompile(`(?i)(password|passwd|pass|pwd|secret|token)=[^&"'\r\n]*`)

	// schemeRe matches the "://" that opens any URI authority, whatever the
	// scheme (amqp, mongodb+srv, ...); matching the separator alone also
	// covers a scheme the rules have never heard of.
	schemeRe = regexp.MustCompile(`://`)
)

// ScrubCredentials returns s with credentials removed. It is what rig/health
// applies to every string it renders in /health/live, /health/ready and
// /health/startup, exported so a service can reuse the same rules for logs
// and client ping errors.
//
// The rules, applied in order:
//
//  1. password=, passwd=, pass=, pwd=, secret= and token= values (any case) are
//     redacted up to the next '&', quote, newline or the end of the string.
//  2. For each scheme:// (amqp, amqps, mongodb, mongodb+srv, redis, rediss,
//     postgres, http, https, or any other), everything from the end of the
//     scheme to the LAST '@' that follows is replaced, whatever it contains
//     (spaces, quotes, '<', '>', '@', ':', '/', even "://"). The host after
//     the '@' is kept, so the operator still sees where: amqp://<redacted>@host:5672.
//     A later scheme:// starts a separate URI only once the earlier one has an
//     '@' followed by a separator (whitespace, quote, ',' or ';'), so two URIs
//     in one string each keep their own host. The one residual gap: a
//     password containing '@', then whitespace or a quote, then "://".
//  3. A scheme-less user:password@host token (delimited by whitespace) has
//     everything before its last '@' replaced.
//
// It over-redacts rather than risk a leak (an email address next to a URL
// without userinfo can lose the URL's host, and a "pass=" inside a longer word
// is redacted), is idempotent, and never panics.
func ScrubCredentials(s string) string {
	if s == "" {
		return s
	}
	s = paramRe.ReplaceAllStringFunc(s, func(m string) string {
		return m[:strings.IndexByte(m, '=')+1] + redacted
	})
	s = scrubSchemeURIs(s)
	return scrubBareUserinfo(s)
}

func isSep(r rune) bool {
	switch r {
	case ' ', '\t', '\r', '\n', '"', '\'', ',', ';':
		return true
	}
	return false
}

func scrubSchemeURIs(s string) string {
	var out strings.Builder
	pos := 0
	for pos < len(s) {
		locs := schemeRe.FindAllStringIndex(s[pos:], -1)
		if len(locs) == 0 {
			break
		}
		start := pos + locs[0][1] // first byte after the scheme://
		end := len(s)
		for _, l := range locs[1:] {
			next := pos + l[0]
			seg := s[start:next]
			if at := strings.LastIndexByte(seg, '@'); at >= 0 && strings.IndexFunc(seg[at+1:], isSep) >= 0 {
				end = next
				break
			}
		}
		at := strings.LastIndexByte(s[start:end], '@')
		if at < 0 {
			// No userinfo anywhere in this region (and, since later regions
			// were absorbed only when they held no '@' either, nowhere after).
			break
		}
		out.WriteString(s[pos:start])
		out.WriteString(redacted)
		pos = start + at // keep the '@' and the host
	}
	out.WriteString(s[pos:])
	return out.String()
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
		out.WriteString(scrubToken(s[i:j]))
		k := j
		for k < len(s) && isSpace(s[k]) {
			k++
		}
		out.WriteString(s[j:k])
		i = k
	}
	return out.String()
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\r' || b == '\n' }

func scrubToken(tok string) string {
	at := strings.LastIndexByte(tok, '@')
	if at < 0 {
		return tok
	}
	prefix := tok[:at]
	if !strings.Contains(prefix, ":") || strings.HasSuffix(prefix, redacted) {
		return tok
	}
	return redacted + tok[at:]
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
