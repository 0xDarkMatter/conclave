package providers

import (
	"strings"
	"testing"
)

// TestEmptyChatContentIsAnError defends the judge against a blank "answer".
// OpenAI-compatible providers (openai, grok, perplexity, glm, OpenRouter) can
// return a choice whose content is empty: a reasoning model that spent its
// whole completion budget (finish_reason "length"), a content filter, a tool
// call. extractChatResponse returned that as success with text "", so the
// judge weighed an empty panel member as a real opinion. Anthropic and Gemini
// parsers already rejected empty text; this brings the shared parser in line.
func TestEmptyChatContentIsAnError(t *testing.T) {
	for _, content := range []string{"", "   \n "} {
		body := `{"choices":[{"message":{"content":` + quoteJSON(content) + `},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":16000,"total_tokens":16010}}`
		text, _, err := extractChatResponse([]byte(body))
		if err == nil {
			t.Fatalf("content %q was accepted as a successful answer %q", content, text)
		}
		if !strings.Contains(err.Error(), "length") {
			t.Fatalf("error %q should carry finish_reason so a truncated budget is recognisable", err)
		}
	}
}

func quoteJSON(s string) string {
	r := strings.NewReplacer(`\`, `\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}
