package e2e_sqlserver

import (
	"context"
	"fmt"
	"os"
	"strconv"

	peersql "github.com/PeerDB-io/peer-flow/connectors/sql"
	connsqlserver "github.com/PeerDB-io/peer-flow/connectors/sqlserver"
	"github.com/PeerDB-io/peer-flow/generated/protos"
	"github.com/PeerDB-io/peer-flow/model"
	"github.com/PeerDB-io/peer-flow/shared"
)

type SQLServerHelper struct {
	peerName string
	config   *protos.SqlServerConfig

	E          peersql.SQLQueryExecutor
	SchemaName string
	tables     []string
}

func NewSQLServerHelper(name string) (*SQLServerHelper, error) {
	port, err := strconv.ParseUint(os.Getenv("SQLSERVER_PORT"), 10, 16)
	if err != nil {
		return nil, fmt.Errorf("invalid SQLSERVER_PORT: %s", os.Getenv("SQLSERVER_PORT"))
	}

	config := &protos.SqlServerConfig{
		Server:   os.Getenv("SQLSERVER_HOST"),
		Port:     uint32(port),
		User:     os.Getenv("SQLSERVER_USER"),
		Password: os.Getenv("SQLSERVER_PASSWORD"),
		Database: os.Getenv("SQLSERVER_DATABASE"),
	}

	connector, err := connsqlserver.NewSQLServerConnector(context.Background(), config)
	if err != nil {
		return nil, err
	}

	connErr := connector.ConnectionActive()
	if connErr != nil {
		return nil, fmt.Errorf("invalid connection configs: %v", connErr)
	}

	rndNum, err := shared.RandomUInt64()
	if err != nil {
		return nil, err
	}

	testSchema := fmt.Sprintf("e2e_test_%d", rndNum)
	err = connector.CreateSchema(testSchema)
	if err != nil {
		return nil, err
	}

	return &SQLServerHelper{
		peerName:   name,
		config:     config,
		E:          connector,
		SchemaName: testSchema,
	}, nil
}

func (h *SQLServerHelper) CreateTable(schema *model.QRecordSchema, tableName string) error {
	err := h.E.CreateTable(schema, h.SchemaName, tableName)
	if err != nil {
		return err
	}

	h.tables = append(h.tables, tableName)
	return nil
}

func (h *SQLServerHelper) GetPeer() *protos.Peer {
	return &protos.Peer{
		Name: h.peerName,
		Type: protos.DBType_SQLSERVER,
		Config: &protos.Peer_SqlserverConfig{
			SqlserverConfig: h.config,
		},
	}
}

func (h *SQLServerHelper) CleanUp() error {
	for _, tbl := range h.tables {
		err := h.E.ExecuteQuery(fmt.Sprintf("DROP TABLE %s.%s", h.SchemaName, tbl))
		if err != nil {
			return err
		}
	}

	if h.SchemaName != "" {
		return h.E.ExecuteQuery(fmt.Sprintf("DROP SCHEMA %s", h.SchemaName))
	}

	return nil
}
