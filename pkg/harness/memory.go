package harness

import (
	"context"
	"github.com/vince-0202/acgo/pkg/communi"
)

type MemoryController struct {
}

func (c MemoryController) RecordWithMetaData(ctx context.Context, msg *communi.Message, metaData map[string]any) {

}

func NewMemoryController() *MemoryController {
	return &MemoryController{}
}
