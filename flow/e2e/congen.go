package e2e

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PeerDB-io/peer-flow/connectors/utils"
	"github.com/PeerDB-io/peer-flow/e2eshared"
	"github.com/PeerDB-io/peer-flow/generated/protos"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

const (
	postgresHost     = "localhost"
	postgresUser     = "postgres"
	postgresPassword = "postgres"
	postgresDatabase = "postgres"
	PostgresPort     = 7132
)

func GetTestPostgresConf() *protos.PostgresConfig {
	return &protos.PostgresConfig{
		Host:     postgresHost,
		Port:     uint32(PostgresPort),
		User:     postgresUser,
		Password: postgresPassword,
		Database: postgresDatabase,
	}
}

func cleanPostgres(pool *pgxpool.Pool, suffix string) error {
	// drop the e2e_test schema with the given suffix if it exists
	_, err := pool.Exec(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS e2e_test_%s CASCADE", suffix))
	if err != nil {
		return fmt.Errorf("failed to drop e2e_test schema: %w", err)
	}

	// drop all open slots with the given suffix
	_, err = pool.Exec(
		context.Background(),
		"SELECT pg_drop_replication_slot(slot_name) FROM pg_replication_slots WHERE slot_name LIKE $1",
		fmt.Sprintf("%%_%s", suffix),
	)
	if err != nil {
		return fmt.Errorf("failed to drop replication slots: %w", err)
	}

	// list all publications from pg_publication table
	rows, err := pool.Query(context.Background(),
		"SELECT pubname FROM pg_publication WHERE pubname LIKE $1",
		fmt.Sprintf("%%_%s", suffix),
	)
	if err != nil {
		return fmt.Errorf("failed to list publications: %w", err)
	}

	// drop all publications with the given suffix
	for rows.Next() {
		var pubName pgtype.Text
		err = rows.Scan(&pubName)
		if err != nil {
			return fmt.Errorf("failed to scan publication name: %w", err)
		}

		_, err = pool.Exec(context.Background(), fmt.Sprintf("DROP PUBLICATION %s", pubName.String))
		if err != nil {
			return fmt.Errorf("failed to drop publication %s: %w", pubName.String, err)
		}
	}

	return nil
}

// setupPostgres sets up the postgres connection pool.
func SetupPostgres(suffix string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), utils.GetPGConnectionString(GetTestPostgresConf()))
	if err != nil {
		return nil, fmt.Errorf("failed to create postgres connection pool: %w", err)
	}

	err = cleanPostgres(pool, suffix)
	if err != nil {
		return nil, err
	}

	setupTx, err := pool.Begin(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to start setup transaction")
	}

	// create an e2e_test schema
	_, err = setupTx.Exec(context.Background(), "SELECT pg_advisory_xact_lock(hashtext('Megaton Mile'))")
	if err != nil {
		return nil, fmt.Errorf("failed to get lock: %w", err)
	}
	defer func() {
		deferErr := setupTx.Rollback(context.Background())
		if deferErr != pgx.ErrTxClosed && deferErr != nil {
			slog.Error("error rolling back setup transaction", slog.Any("error", err))
		}
	}()

	// create an e2e_test schema
	_, err = setupTx.Exec(context.Background(), fmt.Sprintf("CREATE SCHEMA e2e_test_%s", suffix))
	if err != nil {
		return nil, fmt.Errorf("failed to create e2e_test schema: %w", err)
	}

	_, err = setupTx.Exec(context.Background(), `
		CREATE OR REPLACE FUNCTION random_string( int ) RETURNS TEXT as $$
			SELECT string_agg(substring('0123456789bcdfghjkmnpqrstvwxyz',
			round(random() * 30)::integer, 1), '') FROM generate_series(1, $1);
		$$ language sql;
		CREATE OR REPLACE FUNCTION random_bytea(bytea_length integer)
		RETURNS bytea AS $body$
			SELECT decode(string_agg(lpad(to_hex(width_bucket(random(), 0, 1, 256)-1),2,'0'), ''), 'hex')
			FROM generate_series(1, $1);
		$body$
		LANGUAGE 'sql'
		VOLATILE
		SET search_path = 'pg_catalog';
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to create utility functions: %w", err)
	}

	err = setupTx.Commit(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error committing setup transaction: %w", err)
	}

	return pool, nil
}

func TearDownPostgres[T e2eshared.Suite](s T) {
	t := s.T()
	t.Helper()
	pool := s.Pool()
	suffix := s.Suffix()

	if pool != nil {
		t.Log("begin tearing down postgres schema", suffix)
		deadline := time.Now().Add(2 * time.Minute)
		for {
			err := cleanPostgres(pool, suffix)
			if err == nil {
				pool.Close()
				return
			} else if time.Now().After(deadline) {
				require.Fail(t, "failed to teardown postgres schema", suffix)
			}
			time.Sleep(time.Second)
		}
	}
}

// GeneratePostgresPeer generates a postgres peer config for testing.
func GeneratePostgresPeer(postgresPort int) *protos.Peer {
	ret := &protos.Peer{}
	ret.Name = "test_postgres_peer"
	ret.Type = protos.DBType_POSTGRES

	ret.Config = &protos.Peer_PostgresConfig{
		PostgresConfig: &protos.PostgresConfig{
			Host:     "localhost",
			Port:     uint32(postgresPort),
			User:     "postgres",
			Password: "postgres",
			Database: "postgres",
		},
	}

	return ret
}

type FlowConnectionGenerationConfig struct {
	FlowJobName      string
	TableNameMapping map[string]string
	PostgresPort     int
	Destination      *protos.Peer
	CdcStagingPath   string
	SoftDelete       bool
}

// GenerateSnowflakePeer generates a snowflake peer config for testing.
func GenerateSnowflakePeer(snowflakeConfig *protos.SnowflakeConfig) (*protos.Peer, error) {
	ret := &protos.Peer{}
	ret.Name = "test_snowflake_peer"
	ret.Type = protos.DBType_SNOWFLAKE

	ret.Config = &protos.Peer_SnowflakeConfig{
		SnowflakeConfig: snowflakeConfig,
	}

	return ret, nil
}

func (c *FlowConnectionGenerationConfig) GenerateFlowConnectionConfigs() *protos.FlowConnectionConfigs {
	tblMappings := []*protos.TableMapping{}
	for k, v := range c.TableNameMapping {
		tblMappings = append(tblMappings, &protos.TableMapping{
			SourceTableIdentifier:      k,
			DestinationTableIdentifier: v,
		})
	}

	ret := &protos.FlowConnectionConfigs{
		FlowJobName:        c.FlowJobName,
		TableMappings:      tblMappings,
		Source:             GeneratePostgresPeer(c.PostgresPort),
		Destination:        c.Destination,
		CdcStagingPath:     c.CdcStagingPath,
		SoftDelete:         c.SoftDelete,
		SyncedAtColName:    "_PEERDB_SYNCED_AT",
		IdleTimeoutSeconds: 15,
	}
	if ret.SoftDelete {
		ret.SoftDeleteColName = "_PEERDB_IS_DELETED"
	}
	return ret
}

type QRepFlowConnectionGenerationConfig struct {
	FlowJobName                string
	WatermarkTable             string
	DestinationTableIdentifier string
	PostgresPort               int
	Destination                *protos.Peer
	StagingPath                string
}

// GenerateQRepConfig generates a qrep config for testing.
func (c *QRepFlowConnectionGenerationConfig) GenerateQRepConfig(
	query string, watermark string,
) (*protos.QRepConfig, error) {
	ret := &protos.QRepConfig{}
	ret.FlowJobName = c.FlowJobName
	ret.WatermarkTable = c.WatermarkTable
	ret.DestinationTableIdentifier = c.DestinationTableIdentifier

	postgresPeer := GeneratePostgresPeer(c.PostgresPort)
	ret.SourcePeer = postgresPeer

	ret.DestinationPeer = c.Destination

	ret.Query = query
	ret.WatermarkColumn = watermark

	ret.StagingPath = c.StagingPath
	ret.WriteMode = &protos.QRepWriteMode{
		WriteType: protos.QRepWriteType_QREP_WRITE_MODE_APPEND,
	}
	ret.NumRowsPerPartition = 1000

	return ret, nil
}
