package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"yadrotask/internal/models"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) CreatePendingLog(ctx context.Context, sourcePath string, size int64) (int64, error) {
	var id int64
	const query = `INSERT INTO logs (source_path, status, raw_size) VALUES ($1, $2, $3) RETURNING id`
	if err := s.pool.QueryRow(ctx, query, sourcePath, models.LogStatusPending, size).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert log: %w", err)
	}
	return id, nil
}

func (s *Store) MarkLogParsed(ctx context.Context, logID int64, nodesCount, portsCount int) error {
	const query = `UPDATE logs SET status = $2, nodes_count = $3, ports_count = $4, parsed_at = NOW(), error_message = '' WHERE id = $1`
	if _, err := s.pool.Exec(ctx, query, logID, models.LogStatusParsed, nodesCount, portsCount); err != nil {
		return fmt.Errorf("update parsed log: %w", err)
	}
	return nil
}

func (s *Store) MarkLogRejected(ctx context.Context, logID int64, reason string) error {
	const query = `UPDATE logs SET status = $2, error_message = $3 WHERE id = $1`
	if _, err := s.pool.Exec(ctx, query, logID, models.LogStatusRejected, reason); err != nil {
		return fmt.Errorf("update rejected log: %w", err)
	}
	return nil
}

func (s *Store) InsertParsedLog(ctx context.Context, logID int64, parsed models.ParsedLog) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	nodeIDByExternal := map[string]int64{}
	for _, node := range parsed.Nodes {
		var id int64
		const query = `INSERT INTO nodes (log_id, external_id, name, node_type, group_name) VALUES ($1, $2, $3, $4, $5) RETURNING id`
		if err := tx.QueryRow(ctx, query, logID, node.ExternalID, node.Name, node.Type, node.Group).Scan(&id); err != nil {
			return fmt.Errorf("insert node %q: %w", node.ExternalID, err)
		}
		nodeIDByExternal[node.ExternalID] = id
	}

	nodeInfoByExternal := map[string]models.NodeInfoRecord{}
	for _, info := range parsed.Infos {
		nodeInfoByExternal[info.NodeExternalID] = info
	}

	for externalID, nodeID := range nodeIDByExternal {
		info := nodeInfoByExternal[externalID]
		payload := map[string]string{}
		for key, value := range info.Details {
			payload[key] = value
		}
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal node info %q: %w", externalID, err)
		}
		const query = `INSERT INTO nodes_info (node_id, vendor, model, description, details) VALUES ($1, $2, $3, $4, $5)`
		if _, err := tx.Exec(ctx, query, nodeID, info.Vendor, info.Model, info.Description, encoded); err != nil {
			return fmt.Errorf("insert nodes_info %q: %w", externalID, err)
		}
	}

	portIDByExternal := map[string]int64{}
	pendingPeers := make([]struct {
		portID        int64
		peerNodeExtID string
		peerPortExtID string
	}, 0, len(parsed.Ports))
	for _, port := range parsed.Ports {
		nodeID, ok := nodeIDByExternal[port.NodeExternalID]
		if !ok {
			return fmt.Errorf("unknown node %q for port %q", port.NodeExternalID, port.ExternalID)
		}
		var id int64
		const query = `INSERT INTO ports (log_id, node_id, external_id, name, speed) VALUES ($1, $2, $3, $4, $5) RETURNING id`
		if err := tx.QueryRow(ctx, query, logID, nodeID, port.ExternalID, port.Name, port.Speed).Scan(&id); err != nil {
			return fmt.Errorf("insert port %q: %w", port.ExternalID, err)
		}
		portIDByExternal[port.ExternalID] = id
		pendingPeers = append(pendingPeers, struct {
			portID        int64
			peerNodeExtID string
			peerPortExtID string
		}{portID: id, peerNodeExtID: port.PeerNodeExtID, peerPortExtID: port.PeerPortExtID})
	}

	for _, pending := range pendingPeers {
		if pending.peerNodeExtID == "" || pending.peerPortExtID == "" {
			continue
		}
		peerNodeID, ok := nodeIDByExternal[pending.peerNodeExtID]
		if !ok {
			return fmt.Errorf("unknown peer node %q", pending.peerNodeExtID)
		}
		peerPortID, ok := portIDByExternal[pending.peerPortExtID]
		if !ok {
			return fmt.Errorf("unknown peer port %q", pending.peerPortExtID)
		}
		const query = `UPDATE ports SET peer_node_id = $2, peer_port_id = $3 WHERE id = $1`
		if _, err := tx.Exec(ctx, query, pending.portID, peerNodeID, peerPortID); err != nil {
			return fmt.Errorf("update port peer %d: %w", pending.portID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *Store) GetLog(ctx context.Context, logID int64) (models.LogMeta, error) {
	var meta models.LogMeta
	const query = `SELECT id, status, nodes_count, ports_count, uploaded_at, parsed_at, error_message, source_path FROM logs WHERE id = $1`
	if err := s.pool.QueryRow(ctx, query, logID).Scan(&meta.ID, &meta.Status, &meta.NodesCount, &meta.PortsCount, &meta.UploadedAt, &meta.ParsedAt, &meta.Error, &meta.SourcePath); err != nil {
		return models.LogMeta{}, err
	}
	return meta, nil
}

func (s *Store) GetNode(ctx context.Context, nodeID int64) (models.NodeDetails, error) {
	var node models.NodeDetails
	const query = `SELECT id, log_id, external_id, name, node_type, group_name FROM nodes WHERE id = $1`
	if err := s.pool.QueryRow(ctx, query, nodeID).Scan(&node.ID, &node.LogID, &node.ExternalID, &node.Name, &node.Type, &node.Group); err != nil {
		return models.NodeDetails{}, err
	}

	var info models.NodeInfoRecord
	var details []byte
	const infoQuery = `SELECT vendor, model, description, details FROM nodes_info WHERE node_id = $1`
	if err := s.pool.QueryRow(ctx, infoQuery, nodeID).Scan(&info.Vendor, &info.Model, &info.Description, &details); err == nil {
		info.NodeExternalID = node.ExternalID
		_ = json.Unmarshal(details, &info.Details)
	}
	portRows, err := s.pool.Query(ctx, `SELECT id, node_id, external_id, name, speed, peer_node_id, peer_port_id FROM ports WHERE node_id = $1 ORDER BY id`, nodeID)
	if err != nil {
		return models.NodeDetails{}, fmt.Errorf("query ports: %w", err)
	}
	defer portRows.Close()

	for portRows.Next() {
		var port models.PortDetails
		if err := portRows.Scan(&port.ID, &port.NodeID, &port.ExternalID, &port.Name, &port.Speed, &port.PeerNodeID, &port.PeerPortID); err != nil {
			return models.NodeDetails{}, fmt.Errorf("scan port: %w", err)
		}
		node.Ports = append(node.Ports, port)
	}
	node.Info = info
	return node, nil
}

func (s *Store) GetPortsByNode(ctx context.Context, nodeID int64) ([]models.PortDetails, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, node_id, external_id, name, speed, peer_node_id, peer_port_id FROM ports WHERE node_id = $1 ORDER BY id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("query ports: %w", err)
	}
	defer rows.Close()

	ports := make([]models.PortDetails, 0)
	for rows.Next() {
		var port models.PortDetails
		if err := rows.Scan(&port.ID, &port.NodeID, &port.ExternalID, &port.Name, &port.Speed, &port.PeerNodeID, &port.PeerPortID); err != nil {
			return nil, fmt.Errorf("scan port: %w", err)
		}
		ports = append(ports, port)
	}
	return ports, nil
}

func (s *Store) GetTopology(ctx context.Context, logID int64) (models.TopologyResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, external_id, name, node_type, group_name FROM nodes WHERE log_id = $1 ORDER BY id`, logID)
	if err != nil {
		return models.TopologyResponse{}, fmt.Errorf("query nodes: %w", err)
	}
	defer rows.Close()

	nodes := make([]models.NodeSummary, 0)
	groupsByName := map[string][]models.NodeSummary{}
	nodeTypeByID := map[int64]string{}
	for rows.Next() {
		var node models.NodeSummary
		if err := rows.Scan(&node.ID, &node.ExternalID, &node.Name, &node.Type, &node.Group); err != nil {
			return models.TopologyResponse{}, fmt.Errorf("scan node: %w", err)
		}
		nodes = append(nodes, node)
		nodeTypeByID[node.ID] = node.Type
		groupName := node.Group
		if groupName == "" {
			groupName = node.Type
		}
		groupsByName[groupName] = append(groupsByName[groupName], node)
	}

	linkRows, err := s.pool.Query(ctx, `SELECT id, node_id, peer_node_id, peer_port_id FROM ports WHERE log_id = $1 AND peer_node_id IS NOT NULL AND peer_port_id IS NOT NULL ORDER BY id`, logID)
	if err != nil {
		return models.TopologyResponse{}, fmt.Errorf("query links: %w", err)
	}
	defer linkRows.Close()

	links := make([]models.TopologyLink, 0)
	for linkRows.Next() {
		var link models.TopologyLink
		if err := linkRows.Scan(&link.FromPortID, &link.FromNodeID, &link.ToNodeID, &link.ToPortID); err != nil {
			return models.TopologyResponse{}, fmt.Errorf("scan link: %w", err)
		}
		links = append(links, link)
	}

	groupNames := make([]string, 0, len(groupsByName))
	for name := range groupsByName {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)
	groups := make([]models.TopologyGroup, 0, len(groupNames))
	for _, name := range groupNames {
		nodes := groupsByName[name]
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
		groups = append(groups, models.TopologyGroup{Name: name, Type: nodeTypeByID[nodes[0].ID], Nodes: nodes})
	}

	return models.TopologyResponse{LogID: logID, Nodes: nodes, Groups: groups, Links: links}, nil
}

func (s *Store) NodeExists(ctx context.Context, nodeID int64) (bool, error) {
	var id int64
	const query = `SELECT id FROM nodes WHERE id = $1`
	if err := s.pool.QueryRow(ctx, query, nodeID).Scan(&id); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *Store) ParseDuration(_ time.Duration) string {
	return strings.TrimSpace("")
}
