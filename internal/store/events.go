package store

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// emitEvent inserts an audit event. Defined on *Queries so it works both on the
// Store (via embedding) and inside a transaction. A nil detail stores '{}'.
func (q *Queries) EmitEvent(ctx context.Context, domainID uuid.UUID, typ string, detail map[string]any) error {
	var raw []byte
	if detail == nil {
		raw = []byte(`{}`)
	} else {
		b, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		raw = b
	}
	did := domainID
	_, err := q.InsertEvent(ctx, InsertEventParams{
		DomainID: &did,
		Type:     typ,
		Detail:   raw,
	})
	return err
}
