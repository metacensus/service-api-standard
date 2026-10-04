// Package postgres is store.Store over Postgres; see AGENTS.md, "Storage".
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/signing"
	"github.com/metacensus/api/go/store"
	"google.golang.org/protobuf/proto"
)

// Store verifies signatures softly: a user signature must be present for its key_id to resolve, not stand.
type Store struct{ pool *pgxpool.Pool }

var _ store.Store = (*Store)(nil)

// New returns a Store over pool; Migrate must already have run.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) EnrollUser(ctx context.Context, record *v1.UserSigned, publicKey, passwordHash string) error {
	const op = "EnrollUser"
	if _, err := store.ParseID(store.UserID, record.GetId()); err != nil {
		return err
	}
	keyID := record.GetUserSignature().GetKeyId()
	if _, err := signing.EnrolledKey(publicKey, keyID); err != nil {
		return store.InvalidContent
	}
	doc, err := contract.Marshal(record)
	if err != nil {
		return fail(op, err)
	}
	return fail(op, pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, email, password_hash, record) VALUES ($1, $2, $3, $4)`,
			record.GetId(), record.GetContent().GetEmail(), passwordHash, doc); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`INSERT INTO user_keys (key_id, user_id, public_key) VALUES ($1, $2, $3)`,
			keyID, record.GetId(), publicKey)
		return err
	}))
}

func (s *Store) Credential(ctx context.Context, email string) (id, passwordHash string, err error) {
	err = s.pool.QueryRow(ctx, `SELECT id, password_hash FROM users WHERE email = $1`, email).Scan(&id, &passwordHash)
	if kindOf(err) == store.NotFound {
		return "", "", store.Unauthenticated // never reveal whether an email is enrolled
	}
	if err != nil {
		return "", "", fail("Credential", err)
	}
	return id, passwordHash, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*v1.UserSigned, error) {
	return one(ctx, s.pool, "GetUser", &v1.UserSigned{}, `SELECT record FROM users WHERE id = $1`, id)
}

func (s *Store) ListUsers(ctx context.Context) ([]*v1.UserSigned, error) {
	return many(ctx, s.pool, "ListUsers", func() *v1.UserSigned { return &v1.UserSigned{} }, `SELECT record FROM users ORDER BY id`)
}

func (s *Store) CreateTopic(ctx context.Context, callerID string, record *v1.TopicSigned) error {
	return write(ctx, s.pool, "CreateTopic", callerID, record, func(tx pgx.Tx, doc []byte) error {
		if _, err := store.ParseID(store.TopicID, record.GetId()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO topics (id, record) VALUES ($1, $2)`, record.GetId(), doc)
		return err
	})
}

func (s *Store) GetTopic(ctx context.Context, id string) (*v1.TopicSigned, error) {
	return one(ctx, s.pool, "GetTopic", &v1.TopicSigned{}, `SELECT record FROM topics WHERE id = $1`, id)
}

func (s *Store) ListTopics(ctx context.Context) ([]*v1.TopicSigned, error) {
	return many(ctx, s.pool, "ListTopics", func() *v1.TopicSigned { return &v1.TopicSigned{} }, `SELECT record FROM topics ORDER BY id`)
}

func (s *Store) CreateProp(ctx context.Context, callerID string, record *v1.PropSigned) error {
	return write(ctx, s.pool, "CreateProp", callerID, record, func(tx pgx.Tx, doc []byte) error {
		if _, err := store.ParseID(store.PropID, record.GetId()); err != nil {
			return err
		}
		if err := validateProp(record); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO props (topic_id, id, record) VALUES ($1, $2, $3)`,
			record.GetContent().GetTopicId(), record.GetId(), doc)
		return err
	})
}

func (s *Store) GetProp(ctx context.Context, topicID, propID string) (*v1.PropSigned, error) {
	return one(ctx, s.pool, "GetProp", &v1.PropSigned{}, `SELECT record FROM props WHERE topic_id = $1 AND id = $2`, topicID, propID)
}

func (s *Store) ListProps(ctx context.Context, topicID string) ([]*v1.PropSigned, error) {
	return many(ctx, s.pool, "ListProps", func() *v1.PropSigned { return &v1.PropSigned{} }, `SELECT record FROM props WHERE topic_id = $1 ORDER BY id`, topicID)
}

func (s *Store) SetVote(ctx context.Context, callerID string, record *v1.VoteSigned) error {
	return write(ctx, s.pool, "SetVote", callerID, record, func(tx pgx.Tx, doc []byte) error {
		if err := validateVote(callerID, record); err != nil {
			return err
		}
		c := record.GetContent()
		_, err := tx.Exec(ctx, `INSERT INTO votes (topic_id, prop_id, user_id, record) VALUES ($1, $2, $3, $4)
			ON CONFLICT (topic_id, prop_id, user_id) DO UPDATE SET record = excluded.record`,
			c.GetTopicId(), c.GetPropId(), c.GetUserId(), doc)
		return err
	})
}

func (s *Store) ListVotes(ctx context.Context, topicID, propID string) ([]*v1.VoteSigned, error) {
	return many(ctx, s.pool, "ListVotes", func() *v1.VoteSigned { return &v1.VoteSigned{} },
		`SELECT record FROM votes WHERE topic_id = $1 AND prop_id = $2 ORDER BY user_id`, topicID, propID)
}

// signed is a record carrying the signature whose key_id write resolves.
type signed interface {
	proto.Message
	GetUserSignature() *v1.Signature
}

// write runs fn in a transaction after the signature's key_id is shown to
// belong to callerID, so Unauthenticated outranks every other refusal,
// including a record that cannot be marshalled. fn receives the marshalled record.
func write[R signed](ctx context.Context, pool *pgxpool.Pool, op, callerID string, record R, fn func(tx pgx.Tx, doc []byte) error) error {
	return fail(op, pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var owner string
		err := tx.QueryRow(ctx, `SELECT user_id FROM user_keys WHERE key_id = $1`, record.GetUserSignature().GetKeyId()).Scan(&owner)
		if kindOf(err) == store.NotFound || (err == nil && owner != callerID) {
			return store.Unauthenticated
		}
		if err != nil {
			return err
		}
		doc, err := contract.Marshal(record)
		if err != nil {
			return err
		}
		return fn(tx, doc)
	}))
}

// one returns the single record sql selects, or store.NotFound.
func one[M proto.Message](ctx context.Context, pool *pgxpool.Pool, op string, m M, sql string, args ...any) (M, error) {
	var doc []byte
	if err := pool.QueryRow(ctx, sql, args...).Scan(&doc); err != nil {
		return m, fail(op, err)
	}
	return m, fail(op, contract.Unmarshal(doc, m))
}

// many returns every record sql selects, never nil.
func many[M proto.Message](ctx context.Context, pool *pgxpool.Pool, op string, newM func() M, sql string, args ...any) ([]M, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fail(op, err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (M, error) {
		m := newM()
		var doc []byte
		if err := row.Scan(&doc); err != nil {
			return m, err
		}
		return m, contract.Unmarshal(doc, m)
	})
	if out == nil && err == nil {
		out = []M{}
	}
	return out, fail(op, err)
}
