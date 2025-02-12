package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	connpostgres "github.com/PeerDB-io/peer-flow/connectors/postgres"
	"github.com/PeerDB-io/peer-flow/connectors/utils"
	"github.com/PeerDB-io/peer-flow/generated/protos"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
)

func (h *FlowRequestHandler) getPGPeerConfig(ctx context.Context, peerName string) (*protos.PostgresConfig, error) {
	var pgPeerOptions sql.RawBytes
	var pgPeerConfig protos.PostgresConfig
	err := h.pool.QueryRow(ctx,
		"SELECT options FROM peers WHERE name = $1 AND type=3", peerName).Scan(&pgPeerOptions)
	if err != nil {
		return nil, err
	}

	unmarshalErr := proto.Unmarshal(pgPeerOptions, &pgPeerConfig)
	if err != nil {
		return nil, unmarshalErr
	}

	return &pgPeerConfig, nil
}

func (h *FlowRequestHandler) getPoolForPGPeer(ctx context.Context, peerName string) (*pgxpool.Pool, error) {
	pgPeerConfig, err := h.getPGPeerConfig(ctx, peerName)
	if err != nil {
		return nil, err
	}
	connStr := utils.GetPGConnectionString(pgPeerConfig)
	peerPool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		return nil, err
	}
	return peerPool, nil
}

func (h *FlowRequestHandler) GetSchemas(
	ctx context.Context,
	req *protos.PostgresPeerActivityInfoRequest,
) (*protos.PeerSchemasResponse, error) {
	peerPool, err := h.getPoolForPGPeer(ctx, req.PeerName)
	if err != nil {
		return &protos.PeerSchemasResponse{Schemas: nil}, err
	}

	defer peerPool.Close()
	rows, err := peerPool.Query(ctx, "SELECT schema_name"+
		" FROM information_schema.schemata WHERE schema_name !~ '^pg_' AND schema_name <> 'information_schema';")
	if err != nil {
		return &protos.PeerSchemasResponse{Schemas: nil}, err
	}

	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var schema string
		err := rows.Scan(&schema)
		if err != nil {
			return &protos.PeerSchemasResponse{Schemas: nil}, err
		}

		schemas = append(schemas, schema)
	}
	return &protos.PeerSchemasResponse{Schemas: schemas}, nil
}

func (h *FlowRequestHandler) GetTablesInSchema(
	ctx context.Context,
	req *protos.SchemaTablesRequest,
) (*protos.SchemaTablesResponse, error) {
	peerPool, err := h.getPoolForPGPeer(ctx, req.PeerName)
	if err != nil {
		return &protos.SchemaTablesResponse{Tables: nil}, err
	}

	defer peerPool.Close()
	rows, err := peerPool.Query(ctx, `SELECT DISTINCT ON (t.relname)
    t.relname,
    CASE
        WHEN con.contype = 'p' OR t.relreplident = 'i' OR t.relreplident = 'f' THEN true
        ELSE false
    END AS can_mirror
	FROM
		pg_class t
	LEFT JOIN
		pg_namespace n ON t.relnamespace = n.oid
	LEFT JOIN
		pg_constraint con ON con.conrelid = t.oid AND con.contype = 'p'
	WHERE
		n.nspname = $1
	AND
		t.relkind = 'r'
	ORDER BY
    t.relname,
    can_mirror DESC;
`, req.SchemaName)
	if err != nil {
		return &protos.SchemaTablesResponse{Tables: nil}, err
	}

	defer rows.Close()
	var tables []*protos.TableResponse
	for rows.Next() {
		var table pgtype.Text
		var hasPkeyOrReplica pgtype.Bool
		err := rows.Scan(&table, &hasPkeyOrReplica)
		if err != nil {
			return &protos.SchemaTablesResponse{Tables: nil}, err
		}
		canMirror := false
		if hasPkeyOrReplica.Valid && hasPkeyOrReplica.Bool {
			canMirror = true
		}

		tables = append(tables, &protos.TableResponse{
			TableName: table.String,
			CanMirror: canMirror,
		})
	}
	return &protos.SchemaTablesResponse{Tables: tables}, nil
}

// Returns list of tables across schema in schema.table format
func (h *FlowRequestHandler) GetAllTables(
	ctx context.Context,
	req *protos.PostgresPeerActivityInfoRequest,
) (*protos.AllTablesResponse, error) {
	peerPool, err := h.getPoolForPGPeer(ctx, req.PeerName)
	if err != nil {
		return &protos.AllTablesResponse{Tables: nil}, err
	}

	defer peerPool.Close()
	rows, err := peerPool.Query(ctx, "SELECT table_schema || '.' || table_name AS schema_table "+
		"FROM information_schema.tables WHERE table_schema !~ '^pg_' AND table_schema <> 'information_schema'")
	if err != nil {
		return &protos.AllTablesResponse{Tables: nil}, err
	}

	defer rows.Close()
	var tables []string
	for rows.Next() {
		var table pgtype.Text
		err := rows.Scan(&table)
		if err != nil {
			return &protos.AllTablesResponse{Tables: nil}, err
		}

		tables = append(tables, table.String)
	}
	return &protos.AllTablesResponse{Tables: tables}, nil
}

func (h *FlowRequestHandler) GetColumns(
	ctx context.Context,
	req *protos.TableColumnsRequest,
) (*protos.TableColumnsResponse, error) {
	peerPool, err := h.getPoolForPGPeer(ctx, req.PeerName)
	if err != nil {
		return &protos.TableColumnsResponse{Columns: nil}, err
	}

	defer peerPool.Close()
	rows, err := peerPool.Query(ctx, `
	SELECT
    cols.column_name,
    cols.data_type,
    CASE
        WHEN con.contype = 'p' AND cols.ordinal_position = ANY(con.conkey) THEN true
        ELSE false
    END AS is_primary_key
	FROM
		information_schema.columns cols
	JOIN
		pg_class tbl ON cols.table_name = tbl.relname
	JOIN
		pg_namespace n ON tbl.relnamespace = n.oid
	LEFT JOIN
		pg_constraint con ON con.conrelid = tbl.oid
			AND con.contype = 'p'
	WHERE
		n.nspname = $1
		AND cols.table_name = $2
	ORDER BY
    cols.ordinal_position;
	`, req.SchemaName, req.TableName)
	if err != nil {
		return &protos.TableColumnsResponse{Columns: nil}, err
	}

	defer rows.Close()
	var columns []string
	for rows.Next() {
		var columnName pgtype.Text
		var datatype pgtype.Text
		var isPkey pgtype.Bool
		err := rows.Scan(&columnName, &datatype, &isPkey)
		if err != nil {
			return &protos.TableColumnsResponse{Columns: nil}, err
		}
		column := fmt.Sprintf("%s:%s:%v", columnName.String, datatype.String, isPkey.Bool)
		columns = append(columns, column)
	}
	return &protos.TableColumnsResponse{Columns: columns}, nil
}

func (h *FlowRequestHandler) GetSlotInfo(
	ctx context.Context,
	req *protos.PostgresPeerActivityInfoRequest,
) (*protos.PeerSlotResponse, error) {
	pgConfig, err := h.getPGPeerConfig(ctx, req.PeerName)
	if err != nil {
		return &protos.PeerSlotResponse{SlotData: nil}, err
	}

	pgConnector, err := connpostgres.NewPostgresConnector(ctx, pgConfig)
	if err != nil {
		slog.Error("Failed to create postgres connector", slog.Any("error", err))
		return &protos.PeerSlotResponse{SlotData: nil}, err
	}
	defer pgConnector.Close()

	slotInfo, err := pgConnector.GetSlotInfo("")
	if err != nil {
		slog.Error("Failed to get slot info", slog.Any("error", err))
		return &protos.PeerSlotResponse{SlotData: nil}, err
	}

	return &protos.PeerSlotResponse{
		SlotData: slotInfo,
	}, nil
}

func (h *FlowRequestHandler) GetStatInfo(
	ctx context.Context,
	req *protos.PostgresPeerActivityInfoRequest,
) (*protos.PeerStatResponse, error) {
	pgConfig, err := h.getPGPeerConfig(ctx, req.PeerName)
	if err != nil {
		return &protos.PeerStatResponse{StatData: nil}, err
	}

	pgConnector, err := connpostgres.NewPostgresConnector(ctx, pgConfig)
	if err != nil {
		slog.Error("Failed to create postgres connector", slog.Any("error", err))
		return &protos.PeerStatResponse{StatData: nil}, err
	}
	defer pgConnector.Close()

	peerPool := pgConnector.GetPool()
	peerUser := pgConfig.User

	rows, err := peerPool.Query(ctx, "SELECT pid, wait_event, wait_event_type, query_start::text, query,"+
		"EXTRACT(epoch FROM(now()-query_start)) AS dur"+
		" FROM pg_stat_activity WHERE "+
		"usename=$1 AND state != 'idle';", peerUser)
	if err != nil {
		slog.Error("Failed to get stat info", slog.Any("error", err))
		return &protos.PeerStatResponse{StatData: nil}, err
	}
	defer rows.Close()
	var statInfoRows []*protos.StatInfo
	for rows.Next() {
		var pid int64
		var waitEvent sql.NullString
		var waitEventType sql.NullString
		var queryStart sql.NullString
		var query sql.NullString
		var duration sql.NullFloat64

		err := rows.Scan(&pid, &waitEvent, &waitEventType, &queryStart, &query, &duration)
		if err != nil {
			slog.Error("Failed to scan row", slog.Any("error", err))
			return &protos.PeerStatResponse{StatData: nil}, err
		}

		we := waitEvent.String
		if !waitEvent.Valid {
			we = ""
		}

		wet := waitEventType.String
		if !waitEventType.Valid {
			wet = ""
		}

		q := query.String
		if !query.Valid {
			q = ""
		}

		qs := queryStart.String
		if !queryStart.Valid {
			qs = ""
		}

		d := duration.Float64
		if !duration.Valid {
			d = -1
		}

		statInfoRows = append(statInfoRows, &protos.StatInfo{
			Pid:           pid,
			WaitEvent:     we,
			WaitEventType: wet,
			QueryStart:    qs,
			Query:         q,
			Duration:      float32(d),
		})
	}

	return &protos.PeerStatResponse{
		StatData: statInfoRows,
	}, nil
}
