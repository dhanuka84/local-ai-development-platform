package domain

import (
	"context"
	"time"
)

type PackageEvaluation struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	PackageID     string    `json:"package_id"`
	PackageSHA256 string    `json:"package_sha256"`
	Role          string    `json:"role"`
	RegressionID  string    `json:"regression_id"`
	RunIDs        []string  `json:"run_ids"`
	Outcome       string    `json:"outcome"`
	TrialKind     string    `json:"trial_kind"`
	Positive      int       `json:"positive"`
	Negative      int       `json:"negative"`
	Actor         string    `json:"actor"`
	Evidence      Artifact  `json:"evidence"`
	CreatedAt     time.Time `json:"created_at"`
}
type PackageActivation struct {
	ProjectID      string `json:"project_id"`
	TargetID       string `json:"target_id"`
	Role           string `json:"role"`
	PackageID      string `json:"package_id"`
	PackageSHA256  string `json:"package_sha256"`
	EvaluationID   string `json:"evaluation_id"`
	Version        int    `json:"version"`
	Actor          string `json:"actor"`
	Action         string `json:"action"`
	Reason         string `json:"reason"`
	PreviousSHA256 string `json:"previous_sha256"`
}
type PackageRepository interface {
	RecordPackageEvaluation(context.Context, PackageEvaluation) (PackageEvaluation, error)
	GetPackageEvaluation(context.Context, string, string) (PackageEvaluation, error)
	ActivatePackage(context.Context, PackageActivation) (PackageActivation, error)
	GetPackageActivation(context.Context, string, string, string) (PackageActivation, error)
}
