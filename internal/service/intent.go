package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dhanuka84/hybrid-ai-platform/components/workpacket"
	"github.com/dhanuka84/hybrid-ai-platform/internal/domain"
)

type IntentContextInput struct {
	ProjectID      string `json:"project_id"`
	IntentID       string `json:"intent_id"`
	ExpectedSHA256 string `json:"expected_sha256"`
}

// IntentContext never substitutes a newer requirement or drops a constraint to
// fit a prompt. The client must stop on blockers and obtain an attributed new
// version when business meaning changes.
func (s *Service) IntentContext(ctx context.Context, in IntentContextInput) (out domain.IntentContext, err error) {
	intent, err := s.GetProductRecord(ctx, in.ProjectID, in.IntentID, true)
	if err != nil {
		return out, err
	}
	if intent.Kind != "intent" || intent.SHA256 != in.ExpectedSHA256 {
		return out, domain.ErrVersionConflict
	}
	out.Intent = domain.ProductBinding{RecordID: intent.ID, SHA256: intent.SHA256, Kind: intent.Kind}
	out.Specification, err = domain.ParseIntent(intent.Content)
	if err != nil {
		return out, domain.ErrQualityBlocked
	}
	out.Records, out.Blockers = []domain.ProductRecord{}, []string{}
	for _, question := range out.Specification.Clarifications {
		out.Blockers = append(out.Blockers, "Clarification required: "+question)
	}
	for _, assumption := range out.Specification.Assumptions {
		if !assumption.Confirmed {
			out.Blockers = append(out.Blockers, "Unconfirmed assumption: "+assumption.Statement)
		}
	}
	bytes := len(intent.Content)
	for _, binding := range out.Specification.Bindings {
		r, e := s.GetProductRecord(ctx, in.ProjectID, binding.RecordID, true)
		if errors.Is(e, domain.ErrQualityBlocked) || errors.Is(e, ErrForbidden) {
			out.Blockers = append(out.Blockers, "Required context is unavailable or unauthorized: "+binding.RecordID)
			continue
		}
		if e != nil {
			return out, e
		}
		if r.ProductID != intent.ProductID || r.SHA256 != binding.SHA256 || r.Kind != binding.Kind {
			out.Blockers = append(out.Blockers, "Required context no longer matches the accepted binding: "+binding.RecordID)
			continue
		}
		raw, _ := json.Marshal(r)
		bytes += len(raw)
		if bytes > 128*1024 {
			return out, fmt.Errorf("%w: required intent context exceeds 128 KiB; narrow it through a new accepted version", domain.ErrQualityBlocked)
		}
		out.Records = append(out.Records, r)
	}
	for _, criterion := range out.Specification.Criteria {
		if criterion.Oracle == "work_packet" {
			found := false
			for _, record := range out.Records {
				if record.Kind != "test" || domain.Digest([]byte(record.Content)) != criterion.WorkPacketSHA256 {
					continue
				}
				var packet workpacket.Packet
				if json.Unmarshal([]byte(record.Content), &packet) == nil && packet.LocalOnly && !packet.CloudReview && packet.Mode == workpacket.ModePatch && len(packet.Checks) > 0 && workpacket.Evaluate(packet).Allowed {
					found = true
				}
			}
			if !found {
				out.Blockers = append(out.Blockers, "Exact locally executable work packet is not bound as accepted test context: "+criterion.ID)
			}
		}
	}
	out.Ready = len(out.Blockers) == 0
	return out, nil
}
