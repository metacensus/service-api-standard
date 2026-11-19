//go:build integration

package postgres_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
	"github.com/metacensus/api/go/store/storetest"
	"github.com/metacensus/service-api-standard/internal/postgres"
	"github.com/metacensus/service-api-standard/internal/postgres/pgtest"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var policy = signing.ParticipantPolicy(storetest.Origin)

func TestStore_Conformance(t *testing.T) {
	storetest.Run(t, storetest.Harness{
		Open:       func(t *testing.T) store.Store { return postgres.New(pgtest.Shared(t), policy) },
		Signatures: storetest.Hard,
	})
}

func TestStore_ListsEmpty(t *testing.T) {
	s := postgres.New(pgtest.Fresh(t), policy)
	ctx := context.Background()

	tests := []struct {
		name string
		list func() (int, bool, error) // length, non-nil
	}{
		{"success - users", func() (int, bool, error) { r, err := s.ListUsers(ctx); return len(r), r != nil, err }},
		{"success - topics", func() (int, bool, error) { r, err := s.ListTopics(ctx); return len(r), r != nil, err }},
		{"success - props", func() (int, bool, error) { r, err := s.ListProps(ctx, "t"); return len(r), r != nil, err }},
		{"success - votes", func() (int, bool, error) { r, err := s.ListVotes(ctx, "t", "p"); return len(r), r != nil, err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, nonNil, err := tt.list()
			if err != nil || n != 0 || !nonNil {
				t.Errorf("list = (len %d, non-nil %v, %v), want an empty non-nil slice", n, nonNil, err)
			}
		})
	}
}

// Postgres refuses \u0000 in a jsonb string, which no other backend does.
func TestStore_NulInContent(t *testing.T) {
	s := postgres.New(pgtest.Fresh(t), policy)
	ctx := context.Background()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := signing.EncodePublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	userID := store.NewID(store.UserID)
	user := &v1.UserSigned{Id: userID, Content: &v1.User{Email: "u@example.test"}}
	user.Interpretation, user.UserSignature = sign(t, key, user.GetContent())
	if err := s.EnrollUser(ctx, user, pub, "hash"); err != nil {
		t.Fatalf("EnrollUser: %v", err)
	}

	topic := &v1.TopicSigned{Id: store.NewID(store.TopicID), Content: &v1.Topic{Name: "a\x00b"}}
	topic.Interpretation, topic.UserSignature = sign(t, key, topic.GetContent())
	if err := s.CreateTopic(ctx, userID, topic); store.KindOf(err) != store.InvalidContent {
		t.Errorf("CreateTopic = %v, want InvalidContent", err)
	}
}

func TestStore_ClosedPool(t *testing.T) {
	pool := pgtest.Fresh(t)
	s := postgres.New(pool, policy)
	pool.Close()

	if _, err := s.ListTopics(context.Background()); store.KindOf(err) != store.Unavailable {
		t.Errorf("ListTopics = %v, want Unavailable", err)
	}
}

func sign(t *testing.T, key *ecdsa.PrivateKey, content proto.Message) (*v1.Interpretation, *v1.Signature) {
	t.Helper()
	keyID, err := signing.KeyID(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	interp := signing.Interpretation(content)
	at := timestamppb.Now()
	challenge, err := signing.UserChallenge(content, interp, keyID, at)
	if err != nil {
		t.Fatal(err)
	}
	authData := signing.AuthenticatorData(storetest.RPID, signing.FlagUP|signing.FlagUV)
	assertion, err := signing.Assert(key, challenge, authData, signing.ClientData{Type: signing.TypeGet, Origin: storetest.Origin})
	if err != nil {
		t.Fatal(err)
	}
	return interp, &v1.Signature{KeyId: keyID, Time: at, Assertion: assertion}
}
