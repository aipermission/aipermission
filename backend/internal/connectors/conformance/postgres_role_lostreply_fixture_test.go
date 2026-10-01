package conformance_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aipermission/aipermission/backend/internal/connectorresources"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolecatalog"
	"github.com/aipermission/aipermission/backend/internal/connectors/postgres/rolejournal"
	dbpkg "github.com/aipermission/aipermission/backend/internal/db"
	"github.com/aipermission/aipermission/backend/internal/vault"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
)

type postgresLostReplyFixture struct {
	primary     *pgx.Conn
	role, table string
	journal     *rolejournal.Journal
	reopen      func() *rolejournal.Journal
	authority   rolejournal.Anchor
}

func newPostgresLostReplyFixture(t *testing.T) postgresLostReplyFixture {
	t.Helper()
	primary := connectPostgresPolicyFixture(t)
	suffix := fmt.Sprint(time.Now().UnixNano())
	fixture := postgresLostReplyFixture{primary: primary, role: "ap_lost_role_" + suffix, table: "ap_lost_table_" + suffix}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		defer primary.Close(ctx)
		for _, statement := range []string{"DROP TABLE IF EXISTS " + pgx.Identifier{"public", fixture.table}.Sanitize(),
			"DROP ROLE IF EXISTS " + pgx.Identifier{fixture.role}.Sanitize()} {
			if _, err := primary.Exec(ctx, statement); err != nil {
				t.Error(err)
			}
		}
	})
	execPostgresPolicyFixture(t, primary, []string{"CREATE TABLE " + pgx.Identifier{"public", fixture.table}.Sanitize() + " (id integer)",
		"INSERT INTO " + pgx.Identifier{"public", fixture.table}.Sanitize() + " VALUES (42)"})
	fixture.authority = rolejournal.Anchor{TargetID: 900, AdminProfileID: 901, ContextDigest: strings.Repeat("a", 64),
		TargetDigest: strings.Repeat("c", 64), DatabaseName: "aipermission", SuccessorName: "aipermission"}
	path := filepath.Join(t.TempDir(), "private", "lostreply.db")
	var database *sql.DB
	t.Cleanup(func() {
		if database != nil {
			if err := database.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	fixture.reopen = func() *rolejournal.Journal {
		if database != nil {
			if err := database.Close(); err != nil {
				t.Fatal(err)
			}
		}
		var err error
		database, err = dbpkg.OpenEncrypted(path, "lostreply-fixture-password")
		if err != nil {
			t.Fatal(err)
		}
		secretVault, err := vault.New("lostreply-fixture-key")
		if err != nil {
			t.Fatal(err)
		}
		return rolejournal.New(connectorresources.NewStore(database, secretVault, "lostreply-fixture-workspace").Scope("postgres", rolejournal.ResourceKind))
	}
	fixture.journal = fixture.reopen()
	return fixture
}

func (fixture postgresLostReplyFixture) plan() []string {
	role, table := pgx.Identifier{fixture.role}.Sanitize(), pgx.Identifier{"public", fixture.table}.Sanitize()
	return []string{"CREATE ROLE " + role + " LOGIN", "GRANT SELECT ON " + table + " TO " + role, "ALTER TABLE " + table + " OWNER TO " + role}
}

// Only the transport is replaced: pgx and the production catalog transactions
// still send SQL to PostgreSQL. The server's COMMIT response never reaches pgx.
type postgresLostReplyWire struct {
	mu                       sync.Mutex
	armed, offline           bool
	dials, blocked, accepted int
	sql                      []string
	beforeCommit             []rolejournal.Entry
	readErr                  error
	readIntent               func(context.Context) ([]rolejournal.Entry, error)
}

func (wire *postgresLostReplyWire) dial(t *testing.T, config *pgx.ConnConfig) rolecatalog.Dial {
	t.Helper()
	return func(ctx context.Context) (rolecatalog.Connection, error) {
		wire.mu.Lock()
		wire.dials++
		if wire.offline {
			wire.blocked++
			wire.mu.Unlock()
			return nil, errors.New("lostreply fixture observation transport unavailable")
		}
		loseReply := wire.armed
		wire.armed = false
		wire.mu.Unlock()
		connectionConfig := config.Copy()
		if connectionConfig.TLSConfig != nil {
			return nil, errors.New("lostreply fixture requires plaintext disposable PostgreSQL")
		}
		connectionConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
			server, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			if err := server.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
				server.Close()
				return nil, err
			}
			client, proxy := net.Pipe()
			var workers sync.WaitGroup
			workers.Add(2)
			closeBoth := func() { proxy.Close(); server.Close() }
			t.Cleanup(func() { client.Close(); closeBoth(); workers.Wait() })
			go func() {
				defer workers.Done()
				defer closeBoth()
				_ = wire.forwardQueries(proxy, server, loseReply)
			}()
			go func() {
				defer workers.Done()
				defer closeBoth()
				frontend := pgproto3.NewFrontend(server, server)
				for {
					message, err := frontend.Receive()
					if err != nil {
						return
					}
					if completed, ok := message.(*pgproto3.CommandComplete); loseReply && ok && string(completed.CommandTag) == "COMMIT" {
						// Drain ReadyForQuery to prove the real backend completed the
						// transaction, then disconnect without forwarding either reply.
						next, err := frontend.Receive()
						ready, ok := next.(*pgproto3.ReadyForQuery)
						if err == nil && ok && ready.TxStatus == 'I' {
							wire.mu.Lock()
							wire.accepted++
							wire.offline = true
							wire.mu.Unlock()
						}
						return
					}
					encoded, err := message.Encode(nil)
					if err != nil {
						return
					}
					if _, err := io.Copy(proxy, bytes.NewReader(encoded)); err != nil {
						return
					}
				}
			}()
			return client, nil
		}
		return pgx.ConnectConfig(ctx, connectionConfig)
	}
}

func (wire *postgresLostReplyWire) forwardQueries(client, server net.Conn, loseReply bool) error {
	// Startup is untyped; subsequent protocol packets have a one-byte type.
	var startup [4]byte
	if _, err := io.ReadFull(client, startup[:]); err != nil {
		return err
	}
	length := binary.BigEndian.Uint32(startup[:])
	if length < 4 || length > 1<<20 {
		return errors.New("invalid PostgreSQL startup length")
	}
	if _, err := server.Write(startup[:]); err != nil {
		return err
	}
	if _, err := io.CopyN(server, client, int64(length-4)); err != nil {
		return err
	}
	for {
		var header [5]byte
		if _, err := io.ReadFull(client, header[:]); err != nil {
			return err
		}
		length := binary.BigEndian.Uint32(header[1:])
		if length < 4 || length > 1<<20 {
			return errors.New("invalid PostgreSQL message length")
		}
		body := make([]byte, int(length)-4)
		if _, err := io.ReadFull(client, body); err != nil {
			return err
		}
		var statement string
		switch header[0] {
		case 'Q':
			var query pgproto3.Query
			if err := query.Decode(body); err != nil {
				return err
			}
			statement = query.String
		case 'P':
			var parse pgproto3.Parse
			if err := parse.Decode(body); err != nil {
				return err
			}
			statement = parse.Query
		}
		if statement != "" {
			wire.mu.Lock()
			wire.sql = append(wire.sql, statement)
			wire.mu.Unlock()
			if loseReply && strings.EqualFold(strings.TrimSpace(statement), "commit") {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				entries, err := wire.readIntent(ctx)
				cancel()
				wire.mu.Lock()
				wire.beforeCommit, wire.readErr = entries, err
				wire.mu.Unlock()
			}
		}
		if _, err := server.Write(header[:]); err != nil {
			return err
		}
		if _, err := io.Copy(server, bytes.NewReader(body)); err != nil {
			return err
		}
	}
}

func (wire *postgresLostReplyWire) restore() {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	wire.offline = false
}

func (wire *postgresLostReplyWire) mutations() []string {
	wire.mu.Lock()
	defer wire.mu.Unlock()
	var result []string
	for _, statement := range wire.sql {
		upper := strings.ToUpper(strings.TrimSpace(statement))
		for _, prefix := range []string{"CREATE ", "ALTER ", "COMMENT ", "GRANT ", "REASSIGN ", "REVOKE ", "DROP "} {
			if strings.HasPrefix(upper, prefix) {
				result = append(result, statement)
				break
			}
		}
	}
	return result
}
