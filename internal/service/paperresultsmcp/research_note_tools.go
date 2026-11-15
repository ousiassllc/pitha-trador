package paperresultsmcp

import (
	"context"
	"encoding/json"

	"github.com/ousiassllc/pitha-trador/internal/service/paperresultsmcp/researchnote/notetool"
)

func init() {
	for _, s := range notetool.Specs() {
		tools = append(tools, tool{
			name: s.Name, description: s.Description, schema: s.Schema, writes: s.Writes, noPeriod: true,
			decode: func(raw json.RawMessage) (periodArgs, runFunc, *toolError) {
				run, err := s.Decode(raw)
				if err != nil {
					return periodArgs{}, nil, classify(context.Background(), err)
				}
				return periodArgs{}, func(ctx context.Context, e toolEnv) (any, int, error) {
					return run(ctx, notetool.Env{Q: e.q, Now: e.now, Client: e.client, Writer: e.writer})
				}, nil
			},
		})
	}
}
