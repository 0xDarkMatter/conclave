package judge

import (
	"fmt"
	"strings"

	"github.com/0xDarkMatter/conclave-cli/internal/providers"
)

const judgePromptTemplate = `You are arbitrating a multi-model analysis. Synthesize a verdict from the expert analyses below.

## Original Question
%s

## Expert Analyses

%s

## Your Task

Analyze the expert responses and provide a synthesis:

1. Identify points of agreement across models
2. Identify disagreements and evaluate which reasoning is stronger
3. Synthesize a final verdict
4. Rate your confidence (high/medium/low)
5. Provide actionable recommendations

IMPORTANT: Respond ONLY with valid JSON in this exact format (no markdown code blocks, just raw JSON):
{
  "verdict": "SAFE|UNSAFE|YES|NO|UNCERTAIN|{domain-appropriate verdict}",
  "confidence": "high|medium|low",
  "reasoning": "2-3 sentence explanation of the verdict",
  "agreements": ["point 1", "point 2"],
  "disagreements": ["point with resolution"],
  "recommendations": ["action 1", "action 2"]
}`

// expertLabel returns an anonymous label for blind mode
func expertLabel(index int) string {
	if index < 26 {
		return fmt.Sprintf("Expert %c", 'A'+index)
	}
	return fmt.Sprintf("Expert %d", index+1)
}

// BuildPrompt constructs the judge prompt
func BuildPrompt(query string, responses []providers.Response, blind bool) string {
	var analyses []string

	for i, r := range responses {
		var header string
		if blind {
			header = expertLabel(i)
		} else {
			header = fmt.Sprintf("%s (%s)", r.Provider, r.Model)
		}

		if r.Status == "success" {
			analyses = append(analyses, fmt.Sprintf("### %s\n%s", header, r.Response))
		} else {
			analyses = append(analyses, fmt.Sprintf("### %s\n[Error: %s]", header, r.Error))
		}
	}

	return fmt.Sprintf(judgePromptTemplate, query, strings.Join(analyses, "\n\n"))
}
