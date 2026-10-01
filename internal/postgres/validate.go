package postgres

import (
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
)

// validateProp refuses a prop that names no topic; one that names a missing
// topic is the foreign key's to refuse.
func validateProp(p *v1.PropSigned) error {
	if p.GetContent().GetTopicId() == "" {
		return store.InvalidContent
	}
	return nil
}

// validateVote refuses a vote missing an id or cast in someone else's name.
func validateVote(callerID string, v *v1.VoteSigned) error {
	c := v.GetContent()
	if c.GetTopicId() == "" || c.GetPropId() == "" || c.GetUserId() == "" || c.GetUserId() != callerID {
		return store.InvalidContent
	}
	return nil
}
