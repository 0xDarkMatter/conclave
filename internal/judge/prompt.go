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

// BuildPrompt constructs the judge prompt
func BuildPrompt(query string, responses []providers.Response) string {
	var analyses []string

	for _, r := range responses {
		if r.Status == "success" {
			analyses = append(analyses, fmt.Sprintf("### %s (%s)\n%s",
				r.Provider, r.Model, r.Response))
		} else {
			analyses = append(analyses, fmt.Sprintf("### %s (%s)\n[Error: %s]",
				r.Provider, r.Model, r.Error))
		}
	}

	return fmt.Sprintf(judgePromptTemplate, query, strings.Join(analyses, "\n\n"))
}
