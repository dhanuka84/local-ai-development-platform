// SPDX-License-Identifier: MPL-2.0

package codegraph

import "context"

type Request struct {
	RepositoryPath string
	Revision       string
	AllowDirty     bool
	MaxFiles       int
	MaxEntities    int
	MaxRelations   int
}

type Analyzer interface {
	Name() string
	Version() string
	Analyze(context.Context, Request) (Snapshot, error)
}
