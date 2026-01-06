package render

import (
	"fmt"
	"regexp"
	"strings"
)

// Precompiled regex patterns for minification
var (
	// Regex patterns for protected blocks (Go regex doesn't support backreferences)
	preTagRegex      = regexp.MustCompile(`(?is)<pre[^>]*>.*?</pre>`)
	scriptTagRegex   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	styleTagRegex    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	textareaTagRegex = regexp.MustCompile(`(?is)<textarea[^>]*>.*?</textarea>`)

	// IE conditional comments: <!--[if IE]>...<![endif]-->
	ieConditionalRegex = regexp.MustCompile(`(?s)<!--\[if[^\]]*\]>.*?<!\[endif\]-->`)

	// Regular HTML comments (will be removed after IE conditionals are masked)
	htmlCommentRegex = regexp.MustCompile(`(?s)<!--.*?-->`)

	// Regex to collapse multiple spaces/newlines into a single space
	multiSpaceRegex = regexp.MustCompile(`\s+`)

	// Regex to safely remove spaces between BLOCK tags only.
	// Preserves spaces between inline elements like <span>A</span> <span>B</span>
	// List includes common block elements where whitespace is insignificant.
	// Add (?i) at the start to make tag matching case-insensitive
	blockTagRegex = regexp.MustCompile(`(?i)> <(/?(?:div|p|ul|ol|li|table|thead|tbody|tfoot|tr|td|th|section|article|header|footer|nav|aside|main|h[1-6]|blockquote|form|fieldset|noscript|html|head|body|meta|link|title|br|hr))`)
)

// minifyHTML applies safe minification rules.
// It preserves whitespace inside <pre>, <script>, <style>, and <textarea> blocks,
// and maintains spaces between inline elements to prevent text from collapsing.
func minifyHTML(html string) string {
	// 1. Masking Phase: Hide content that must NOT be minified
	var placeholders []string

	maskFunc := func(match string) string {
		token := fmt.Sprintf("___RIG_TOKEN_%d___", len(placeholders))
		placeholders = append(placeholders, match)
		return token
	}

	maskedHTML := html
	maskedHTML = preTagRegex.ReplaceAllStringFunc(maskedHTML, maskFunc)
	maskedHTML = scriptTagRegex.ReplaceAllStringFunc(maskedHTML, maskFunc)
	maskedHTML = styleTagRegex.ReplaceAllStringFunc(maskedHTML, maskFunc)
	maskedHTML = textareaTagRegex.ReplaceAllStringFunc(maskedHTML, maskFunc)
	// Preserve IE conditional comments
	maskedHTML = ieConditionalRegex.ReplaceAllStringFunc(maskedHTML, maskFunc)

	// 2. Minification Phase

	// Remove HTML comments (IE conditionals already masked)
	minified := htmlCommentRegex.ReplaceAllString(maskedHTML, "")

	// Collapse all whitespace (newlines, tabs) to a single space
	// This turns "<div>  \n  <span>" into "<div> <span>"
	minified = multiSpaceRegex.ReplaceAllString(minified, " ")

	// Remove the remaining single space ONLY between block tags
	// This turns "</div> <div>" into "</div><div>"
	// But keeps "</span> <span>" as "</span> <span>"
	minified = blockTagRegex.ReplaceAllString(minified, "><$1")

	// Trim outer whitespace
	minified = strings.TrimSpace(minified)

	// 3. Unmasking Phase: Restore the sensitive content
	for i, originalContent := range placeholders {
		token := fmt.Sprintf("___RIG_TOKEN_%d___", i)
		minified = strings.Replace(minified, token, originalContent, 1)
	}

	return minified
}
