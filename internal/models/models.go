package models

import "time"

type LogStatus string

const (
	LogStatusPending  LogStatus = "pending"
	LogStatusParsed   LogStatus = "parsed"
	LogStatusRejected LogStatus = "rejected"
)

type ParsedLog struct {
	SourcePath string
	Nodes      []NodeRecord
	Ports      []PortRecord
	Infos      []NodeInfoRecord
}

type NodeRecord struct {
	ExternalID string
	Name       string
	Type       string
	Group      string
}

type PortRecord struct {
	ExternalID     string
	NodeExternalID string
	Name           string
	Speed          string
	PeerNodeExtID  string
	PeerPortExtID  string
}

type NodeInfoRecord struct {
	NodeExternalID string
	Vendor         string
	Model          string
	Description    string
	Details        map[string]string
}

type LogMeta struct {
	ID         int64      `json:"log_id"`
	Status     LogStatus  `json:"status"`
	NodesCount int        `json:"nodes_count"`
	PortsCount int        `json:"ports_count"`
	UploadedAt time.Time  `json:"uploaded_at"`
	ParsedAt   *time.Time `json:"parsed_at,omitempty"`
	Error      string     `json:"error,omitempty"`
	SourcePath string     `json:"source_path"`
}

type NodeDetails struct {
	ID         int64          `json:"node_id"`
	LogID      int64          `json:"log_id"`
	ExternalID string         `json:"external_id"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Group      string         `json:"group"`
	Info       NodeInfoRecord `json:"info"`
	Ports      []PortDetails  `json:"ports"`
}

type PortDetails struct {
	ID         int64  `json:"port_id"`
	NodeID     int64  `json:"node_id"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Speed      string `json:"speed"`
	PeerNodeID *int64 `json:"peer_node_id,omitempty"`
	PeerPortID *int64 `json:"peer_port_id,omitempty"`
}

type TopologyGroup struct {
	Name  string        `json:"name"`
	Type  string        `json:"type"`
	Nodes []NodeSummary `json:"nodes"`
}

type NodeSummary struct {
	ID         int64  `json:"node_id"`
	ExternalID string `json:"external_id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	Group      string `json:"group"`
}

type TopologyResponse struct {
	LogID  int64           `json:"log_id"`
	Nodes  []NodeSummary   `json:"nodes"`
	Groups []TopologyGroup `json:"groups"`
	Links  []TopologyLink  `json:"links"`
}

type TopologyLink struct {
	FromPortID int64 `json:"from_port_id"`
	ToPortID   int64 `json:"to_port_id"`
	FromNodeID int64 `json:"from_node_id"`
	ToNodeID   int64 `json:"to_node_id"`
}
