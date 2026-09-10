package service

import (
	"fmt"
	"strings"
	"testing"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func TestTextQualitySignals(t *testing.T) {
	for _, tc := range []struct{ text, flag string }{
		{" \n\t", "empty_text"}, {"bad\xfftext", "invalid_utf8"}, {"replaced \ufffd character", "replacement_character_requires_review"}, {"control\x01text", "unexpected_control_character"}, {"unfinished...", "possible_truncation_requires_review"}, {strings.Repeat("a", domain.QualityTextLimit+1), "text_exceeds_inspection_limit"},
	} {
		t.Run(tc.flag, func(t *testing.T) {
			if !strings.Contains(strings.Join(textFlags(tc.text), ","), tc.flag) {
				t.Fatal("missing quality signal")
			}
		})
	}
	if got := textFlags("Unicode café and Straße\nwith normal\twhitespace."); len(got) != 0 {
		t.Fatal("valid text flagged", got)
	}
}

func TestLexicalDuplicateEvaluationDoesNotClaimMeaning(t *testing.T) {
	if textSimilarity("Normalize  Unicode whitespace.", "NORMALIZE\nUnicode whitespace.") != 1 {
		t.Fatal("case and whitespace duplicate missed")
	}
	if textSimilarity("Normalize whitespace consistently.", "Archive database backups monthly.") >= .85 {
		t.Fatal("unrelated fixture marked similar")
	}
	words := []string{}
	for i := 0; i < 80; i++ {
		words = append(words, fmt.Sprintf("term%d", i))
	}
	before := strings.Join(words, " ") + " always enable the feature"
	after := strings.Join(words, " ") + " never enable the feature"
	// This deliberately contradictory pair is lexically close. The API exposes
	// a review candidate, not equivalence or permission to merge/publish it.
	if textSimilarity(before, after) < .85 {
		t.Fatal("fixture no longer demonstrates why lexical scores need review")
	}
	if textSimilarity("!!!", "???") != 0 {
		t.Fatal("empty token sets manufactured certainty")
	}
	terms := qualityTerms(`one OR two "quoted" ' sql -- expression; café ` + strings.Repeat("word ", 40))
	if len(terms) > 16 || !strings.Contains(strings.Join(terms, " "), `"or"`) {
		t.Fatal("query terms are not bounded literal tokens")
	}
}
