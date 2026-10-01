//go:build integration

// Package integration exercises the built image, not the handlers.
package integration

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	contract "github.com/metacensus/api/go/contract"
	v1 "github.com/metacensus/api/go/metacensus/v1"
	"github.com/metacensus/api/go/server/routes"
	"github.com/metacensus/api/go/signing"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	servicePort = "3001/tcp"

	postgresImage = "postgres:18-alpine"
	// The database's name on the shared network, and its throwaway credentials.
	dbHost = "postgres"
	dbURL  = "postgres://service:service@" + dbHost + ":5432/service?sslmode=disable"

	// What a participant's assertions carry. The Postgres store checks that an
	// assertion is present, not that it stands (storetest.Soft), so these only
	// need to be self-consistent.
	rpID   = "integration.invalid"
	origin = "https://" + rpID

	bootTimeout = 90 * time.Second
)

// serviceRequest is the container request for the image under test, preferring
// a prebuilt image so CI does not build twice.
func serviceRequest(env map[string]string, waitFor wait.Strategy) testcontainers.ContainerRequest {
	req := testcontainers.ContainerRequest{
		ExposedPorts: []string{servicePort},
		Env:          env,
		WaitingFor:   waitFor,
	}
	if image := os.Getenv("SERVICE_IMAGE"); image != "" {
		req.Image = image
	} else {
		req.FromDockerfile = testcontainers.FromDockerfile{
			Context:    "..",
			Dockerfile: "Dockerfile",
			KeepImage:  true,
		}
	}
	return req
}

// start runs req, and on failure prints the container's logs before it is
// terminated.
func start(t *testing.T, ctx context.Context, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if container != nil {
		t.Cleanup(func() {
			if t.Failed() {
				if logs, err := container.Logs(context.Background()); err == nil {
					body, _ := io.ReadAll(logs)
					t.Logf("container logs (%s):\n%s", req.Image, body)
				}
			}
			_ = container.Terminate(context.Background())
		})
	}
	if err != nil {
		t.Fatalf("start container: %v", err)
	}
	return container
}

func newNetwork(t *testing.T, ctx context.Context) string {
	t.Helper()
	net, err := network.New(ctx)
	if err != nil {
		t.Fatalf("create network: %v", err)
	}
	t.Cleanup(func() { _ = net.Remove(context.Background()) })
	return net.Name
}

func startPostgres(t *testing.T, ctx context.Context, netName string) {
	t.Helper()
	start(t, ctx, testcontainers.ContainerRequest{
		Image: postgresImage,
		Env: map[string]string{
			"POSTGRES_USER":     "service",
			"POSTGRES_PASSWORD": "service",
			"POSTGRES_DB":       "service",
		},
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {dbHost}},
		// The image starts a temporary server to initialise, then restarts:
		// ready is the second time it says so.
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).WithStartupTimeout(bootTimeout),
	})
}

// TestRefusesToBoot: a service that serves against no database would answer
// every request with a 5xx, so it must fail at startup instead.
func TestRefusesToBoot(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"error - no DATABASE_URL", map[string]string{}},
		{"error - Postgres unreachable", map[string]string{
			"DATABASE_URL": "postgres://service:service@127.0.0.1:1/service?sslmode=disable&connect_timeout=3",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			container := start(t, ctx, serviceRequest(tc.env, wait.ForExit().WithExitTimeout(bootTimeout)))

			state, err := container.State(ctx)
			if err != nil {
				t.Fatalf("container state: %v", err)
			}
			if state.ExitCode == 0 {
				t.Errorf("exit code = 0, want non-zero")
			}
		})
	}
}

// TestServes boots the image against a real Postgres and drives the API over
// HTTP. The journey only works if migrations ran at boot.
func TestServes(t *testing.T) {
	ctx := context.Background()
	netName := newNetwork(t, ctx)
	startPostgres(t, ctx, netName)

	req := serviceRequest(map[string]string{"DATABASE_URL": dbURL},
		wait.ForHTTP("/healthz").WithPort(servicePort).WithStartupTimeout(bootTimeout))
	req.Networks = []string{netName}
	container := start(t, ctx, req)

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("container host: %v", err)
	}
	port, err := container.MappedPort(ctx, servicePort)
	if err != nil {
		t.Fatalf("mapped port: %v", err)
	}
	api := &client{t: t, base: fmt.Sprintf("http://%s:%s", host, port.Port())}

	t.Run("success - the liveness probe answers", func(t *testing.T) {
		status, _ := api.send(http.MethodGet, "/healthz", "", nil)
		if status != http.StatusOK {
			t.Errorf("status = %d, want 200", status)
		}
	})

	t.Run("error - the API refuses a caller with no session", func(t *testing.T) {
		status, _ := api.send(http.MethodGet, routes.Prefix+"/self", "", nil)
		if status != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", status)
		}
	})

	t.Run("success - sign up, then a topic, a prop and a vote", func(t *testing.T) {
		me := newParticipant(t)

		user := &v1.User{Name: "Ada", Email: "ada@integration.invalid", Country: "GB"}
		interp, sig := me.sign(user)
		var session v1.Session
		api.call(http.MethodPost, "/signup", "", &v1.SignUpRequest{
			Content:        user,
			Password:       "correct horse battery staple",
			Interpretation: interp,
			PublicKey:      me.publicKey,
			UserSignature:  sig,
		}, &session)
		token := session.GetToken()
		if token == "" {
			t.Fatal("sign-up returned no access token")
		}

		var self v1.UserSigned
		api.call(http.MethodGet, "/self", token, nil, &self)
		if self.GetContent().GetEmail() != user.GetEmail() {
			t.Fatalf("self email = %q, want %q", self.GetContent().GetEmail(), user.GetEmail())
		}

		topic := &v1.Topic{Name: "Elections", Description: "voting reform"}
		interp, sig = me.sign(topic)
		var topicRec v1.TopicSigned
		api.call(http.MethodPost, "/topic", token, &v1.TopicCreateRequest{
			Content: topic, Interpretation: interp, UserSignature: sig,
		}, &topicRec)
		topicID := topicRec.GetId()

		prop := &v1.Prop{TopicId: topicID, Type: v1.Prop_Statement, Description: "ranked choice"}
		interp, sig = me.sign(prop)
		var propRec v1.PropSigned
		api.call(http.MethodPost, "/topic/"+topicID+"/prop", token, &v1.PropCreateRequest{
			TopicId: topicID, Content: prop, Interpretation: interp, UserSignature: sig,
		}, &propRec)
		propID := propRec.GetId()

		votePath := "/topic/" + topicID + "/prop/" + propID + "/vote"
		vote := &v1.Vote{TopicId: topicID, PropId: propID, UserId: self.GetId(), Position: v1.Vote_For}
		interp, sig = me.sign(vote)
		api.call(http.MethodPost, votePath, token, &v1.VoteSetRequest{
			TopicId: topicID, PropId: propID, Content: vote, Interpretation: interp, UserSignature: sig,
		}, &v1.VoteSigned{})

		var votes v1.VoteList
		api.call(http.MethodGet, votePath, token, nil, &votes)
		if len(votes.GetItems()) != 1 {
			t.Fatalf("votes = %d, want 1", len(votes.GetItems()))
		}
		got := votes.GetItems()[0].GetContent()
		if got.GetUserId() != self.GetId() || got.GetPosition() != v1.Vote_For {
			t.Errorf("vote = %v, want a For vote by %s", got, self.GetId())
		}
	})
}

type client struct {
	t    *testing.T
	base string
}

func (c *client) send(method, path, token string, body []byte) (int, []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, bytes.NewReader(body))
	if err != nil {
		c.t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read %s %s: %v", method, path, err)
	}
	return resp.StatusCode, out
}

// call sends in as protojson, requires 200, decodes into out.
func (c *client) call(method, path, token string, in, out proto.Message) {
	c.t.Helper()
	var body []byte
	if in != nil {
		var err error
		if body, err = contract.Marshal(in); err != nil {
			c.t.Fatalf("marshal %s %s: %v", method, path, err)
		}
	}
	status, got := c.send(method, routes.Prefix+path, token, body)
	if status != http.StatusOK {
		c.t.Fatalf("%s %s: status %d, body %s", method, path, status, got)
	}
	if err := contract.Unmarshal(got, out); err != nil {
		c.t.Fatalf("decode %s %s: %v", method, path, err)
	}
}

type participant struct {
	t         *testing.T
	key       *ecdsa.PrivateKey
	keyID     string
	publicKey string // as SignUpRequest.public_key carries it
}

func newParticipant(t *testing.T) *participant {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyID, err := signing.KeyID(&key.PublicKey)
	if err != nil {
		t.Fatalf("thumbprint key: %v", err)
	}
	publicKey, err := signing.EncodePublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	return &participant{t: t, key: key, keyID: keyID, publicKey: publicKey}
}

// sign produces the interpretation and a signature over content, shaped like
// a user-verified passkey assertion.
func (p *participant) sign(content proto.Message) (*v1.Interpretation, *v1.Signature) {
	p.t.Helper()
	interp := signing.Interpretation(content)
	at := timestamppb.Now()
	challenge, err := signing.UserChallenge(content, interp, p.keyID, at)
	if err != nil {
		p.t.Fatalf("compute challenge: %v", err)
	}
	authData := signing.AuthenticatorData(rpID, signing.FlagUP|signing.FlagUV)
	assertion, err := signing.Assert(p.key, challenge, authData, signing.ClientData{Type: signing.TypeGet, Origin: origin})
	if err != nil {
		p.t.Fatalf("assert: %v", err)
	}
	return interp, &v1.Signature{KeyId: p.keyID, Time: at, Assertion: assertion}
}
