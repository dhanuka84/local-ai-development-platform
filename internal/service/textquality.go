package service

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

func (s *Service) InspectCandidateQuality(ctx context.Context, id string, version int) (out domain.CandidateQualityAssessment, err error) {
	item, err := s.repository.GetKnowledge(ctx, id, true)
	if err != nil {
		return out, err
	}
	if _, err = s.AuthorizeProjectAction(ctx, item.ProjectID, "knowledge_candidate", item.ID, "read", map[string]any{"status": item.Status}); err != nil {
		return out, err
	}
	if version < 1 || item.Version != version {
		return out, domain.ErrVersionConflict
	}
	out = domain.CandidateQualityAssessment{KnowledgeID: item.ID, Version: item.Version, ContentSHA256: domain.Digest([]byte(item.Content)), Flags: textFlags(item.Content), PossibleDuplicates: []domain.SimilarKnowledge{}, Limit: 100, Algorithm: "token-bigram-jaccard-v1", Coverage: "Advisory text inspection of up to 100 eligible PostgreSQL lexical candidates, each at most 64 KiB. Similarity is not semantic equivalence; no matches does not establish recall, validation or approval. Source and projection drift remain governed by the existing freshness gates."}
	if len(item.Content) > domain.QualityTextLimit {
		return out, nil
	}
	terms := qualityTerms(item.Content)
	if len(terms) == 0 {
		return out, nil
	}
	r, ok := s.repository.(domain.TextQualityRepository)
	if !ok {
		return out, domain.ErrEvidenceUnavailable
	}
	candidates, err := r.FindQualityCandidates(ctx, item.ProjectID, strings.Join(terms, " OR "), out.Limit)
	if err != nil {
		return out, err
	}
	for _, candidate := range candidates {
		if candidate.ID == item.ID || candidate.ProjectID != item.ProjectID || candidate.Status != domain.CandidateApproved || len(candidate.Content) > domain.QualityTextLimit {
			continue
		}
		out.Scanned++
		score := textSimilarity(item.Content, candidate.Content)
		if score >= 0.85 {
			out.PossibleDuplicates = append(out.PossibleDuplicates, domain.SimilarKnowledge{KnowledgeID: candidate.ID, Version: candidate.Version, ContentSHA256: domain.Digest([]byte(candidate.Content)), LexicalSimilarity: score})
		}
	}
	sort.Slice(out.PossibleDuplicates, func(i, j int) bool {
		a, b := out.PossibleDuplicates[i], out.PossibleDuplicates[j]
		if a.LexicalSimilarity == b.LexicalSimilarity {
			return a.KnowledgeID < b.KnowledgeID
		}
		return a.LexicalSimilarity > b.LexicalSimilarity
	})
	if len(out.PossibleDuplicates) > 0 {
		out.Flags = append(out.Flags, "possible_duplicate_requires_review")
	}
	return out, nil
}

func textFlags(text string) []string {
	flags := []string{}
	if strings.TrimSpace(text) == "" {
		flags = append(flags, "empty_text")
	}
	if len(text) > domain.QualityTextLimit {
		return append(flags, "text_exceeds_inspection_limit")
	}
	if !utf8.ValidString(text) {
		flags = append(flags, "invalid_utf8")
	}
	if strings.ContainsRune(text, utf8.RuneError) {
		flags = append(flags, "replacement_character_requires_review")
	}
	for _, r := range text {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			flags = append(flags, "unexpected_control_character")
			break
		}
	}
	if t := strings.TrimSpace(text); strings.HasSuffix(t, "...") || strings.HasSuffix(t, "…") {
		flags = append(flags, "possible_truncation_requires_review")
	}
	return flags
}

func qualityTokens(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '_' })
}

func qualityTerms(text string) []string {
	terms := []string{}
	seen := map[string]bool{}
	for _, word := range qualityTokens(text) {
		if seen[word] || len(word) > 128 {
			continue
		}
		seen[word] = true
		terms = append(terms, `"`+word+`"`)
		if len(terms) == 16 {
			break
		}
	}
	return terms
}

func textSimilarity(a, b string) float64 {
	shingles := func(text string) map[string]bool {
		words := qualityTokens(text)
		set := map[string]bool{}
		if len(words) == 1 {
			set[words[0]] = true
		}
		for i := 1; i < len(words); i++ {
			set[words[i-1]+"\x00"+words[i]] = true
		}
		return set
	}
	x, y := shingles(a), shingles(b)
	intersection := 0
	for word := range x {
		if y[word] {
			intersection++
		}
	}
	union := len(x) + len(y) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}
