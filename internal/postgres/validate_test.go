package postgres

import (
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/store"
)

func TestValidateProp_Prop(t *testing.T) {
	prop := func(topicID string) *v1.PropSigned {
		return &v1.PropSigned{Content: &v1.Prop{TopicId: topicID}}
	}
	tests := []struct {
		name string
		prop *v1.PropSigned
		want error
	}{
		{"success - names a topic", prop("t"), nil},
		{"error - empty topic id", prop(""), store.InvalidContent},
		{"error - no content", &v1.PropSigned{}, store.InvalidContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateProp(tt.prop); got != tt.want {
				t.Errorf("validateProp = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateVote_Vote(t *testing.T) {
	vote := func(topic, prop, user string) *v1.VoteSigned {
		return &v1.VoteSigned{Content: &v1.Vote{TopicId: topic, PropId: prop, UserId: user}}
	}
	tests := []struct {
		name string
		vote *v1.VoteSigned
		want error
	}{
		{"success - caller's own vote", vote("t", "p", "me"), nil},
		{"error - empty topic id", vote("", "p", "me"), store.InvalidContent},
		{"error - empty prop id", vote("t", "", "me"), store.InvalidContent},
		{"error - empty user id", vote("t", "p", ""), store.InvalidContent},
		{"error - someone else's vote", vote("t", "p", "other"), store.InvalidContent},
		{"error - no content", &v1.VoteSigned{}, store.InvalidContent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateVote("me", tt.vote); got != tt.want {
				t.Errorf("validateVote = %v, want %v", got, tt.want)
			}
		})
	}
}
