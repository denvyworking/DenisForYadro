package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"yadrotask/internal/models"
)

var sectionHeader = regexp.MustCompile(`^(?:\[\s*([a-zA-Z0-9_\-]+)\s*\]|(?:#\s*)?section\s*:\s*([a-zA-Z0-9_\-]+)|---\s*([a-zA-Z0-9_\-]+)\s*---)$`)

func Parse(sourcePath string, data []byte) (models.ParsedLog, error) {
	sections, err := splitSections(data)
	if err != nil {
		return models.ParsedLog{}, err
	}

	nodeRecords, err := parseNodes(sections["nodes"])
	if err != nil {
		return models.ParsedLog{}, err
	}
	portRecords, err := parsePorts(sections["ports"])
	if err != nil {
		return models.ParsedLog{}, err
	}
	infoRecords, err := parseInfos(sections["nodes_info"])
	if err != nil {
		return models.ParsedLog{}, err
	}

	if len(nodeRecords) == 0 {
		return models.ParsedLog{}, fmt.Errorf("nodes section is empty")
	}
	if len(portRecords) == 0 {
		return models.ParsedLog{}, fmt.Errorf("ports section is empty")
	}
	if len(infoRecords) == 0 {
		return models.ParsedLog{}, fmt.Errorf("nodes_info section is empty")
	}

	seenNodes := map[string]struct{}{}
	for _, node := range nodeRecords {
		if _, exists := seenNodes[node.ExternalID]; exists {
			return models.ParsedLog{}, fmt.Errorf("duplicate node id %q", node.ExternalID)
		}
		seenNodes[node.ExternalID] = struct{}{}
	}

	seenPorts := map[string]struct{}{}
	for _, port := range portRecords {
		if _, exists := seenPorts[port.ExternalID]; exists {
			return models.ParsedLog{}, fmt.Errorf("duplicate port id %q", port.ExternalID)
		}
		seenPorts[port.ExternalID] = struct{}{}
		if _, ok := seenNodes[port.NodeExternalID]; !ok {
			return models.ParsedLog{}, fmt.Errorf("port %q references unknown node %q", port.ExternalID, port.NodeExternalID)
		}
		if port.PeerNodeExtID != "" {
			if _, ok := seenNodes[port.PeerNodeExtID]; !ok {
				return models.ParsedLog{}, fmt.Errorf("port %q references unknown peer node %q", port.ExternalID, port.PeerNodeExtID)
			}
		}
	}

	for _, info := range infoRecords {
		if _, ok := seenNodes[info.NodeExternalID]; !ok {
			return models.ParsedLog{}, fmt.Errorf("nodes_info references unknown node %q", info.NodeExternalID)
		}
	}

	return models.ParsedLog{SourcePath: sourcePath, Nodes: nodeRecords, Ports: portRecords, Infos: infoRecords}, nil
}

func splitSections(data []byte) (map[string][]map[string]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)

	sections := map[string][]map[string]string{}
	current := ""
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(strings.ToLower(line), "# section:") {
			continue
		}
		if match := sectionHeader.FindStringSubmatch(line); match != nil {
			for _, candidate := range match[1:] {
				if candidate != "" {
					current = normalizeSection(candidate)
					sections[current] = sections[current]
					break
				}
			}
			continue
		}
		if current == "" {
			return nil, fmt.Errorf("line %d: record outside of section", lineNum)
		}
		record, err := parseRecord(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		sections[current] = append(sections[current], record)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return sections, nil
}

func normalizeSection(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseRecord(line string) (map[string]string, error) {
	if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return nil, fmt.Errorf("invalid json record: %w", err)
		}
		return normalizeMap(raw), nil
	}

	fields := map[string]string{}
	currentKey := ""
	for _, token := range splitTokens(line) {
		if token == "" {
			continue
		}
		key, value, ok := strings.Cut(token, "=")
		if ok {
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" || value == "" {
				return nil, fmt.Errorf("invalid token %q", token)
			}
			currentKey = strings.ToLower(key)
			fields[currentKey] = trimQuotes(value)
			continue
		}
		if currentKey == "" {
			return nil, fmt.Errorf("invalid token %q", token)
		}
		fields[currentKey] = fields[currentKey] + " " + trimQuotes(token)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty record")
	}
	return fields, nil
}

func normalizeMap(raw map[string]any) map[string]string {
	result := map[string]string{}
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			result[strings.ToLower(key)] = typed
		case float64:
			result[strings.ToLower(key)] = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", typed), "0"), ".")
		case bool:
			if typed {
				result[strings.ToLower(key)] = "true"
			} else {
				result[strings.ToLower(key)] = "false"
			}
		case nil:
			result[strings.ToLower(key)] = ""
		default:
			result[strings.ToLower(key)] = fmt.Sprint(typed)
		}
	}
	return result
}

func splitTokens(line string) []string {
	var tokens []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case inQuotes && ch == quoteChar:
			inQuotes = false
		case !inQuotes && (ch == '"' || ch == '\''):
			inQuotes = true
			quoteChar = ch
		case !inQuotes && (ch == ' ' || ch == '\t' || ch == ',' || ch == ';'):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

func trimQuotes(value string) string {
	if len(value) >= 2 {
		if (strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"")) || (strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'")) {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func parseNodes(records []map[string]string) ([]models.NodeRecord, error) {
	result := make([]models.NodeRecord, 0, len(records))
	for idx, record := range records {
		nodeID := firstValue(record, "id", "node_id", "external_id")
		nodeType := firstValue(record, "type", "node_type")
		name := firstValue(record, "name", "label")
		group := firstValue(record, "group", "group_name")
		if nodeID == "" || nodeType == "" || name == "" {
			return nil, fmt.Errorf("nodes record %d missing required fields", idx+1)
		}
		result = append(result, models.NodeRecord{ExternalID: nodeID, Type: nodeType, Name: name, Group: group})
	}
	return result, nil
}

func parsePorts(records []map[string]string) ([]models.PortRecord, error) {
	result := make([]models.PortRecord, 0, len(records))
	for idx, record := range records {
		portID := firstValue(record, "id", "port_id", "external_id")
		nodeID := firstValue(record, "node_id", "node")
		name := firstValue(record, "name", "label")
		if portID == "" || nodeID == "" || name == "" {
			return nil, fmt.Errorf("ports record %d missing required fields", idx+1)
		}
		result = append(result, models.PortRecord{
			ExternalID:     portID,
			NodeExternalID: nodeID,
			Name:           name,
			Speed:          firstValue(record, "speed"),
			PeerNodeExtID:  firstValue(record, "peer_node_id", "peer_node"),
			PeerPortExtID:  firstValue(record, "peer_port_id", "peer_port"),
		})
	}
	return result, nil
}

func parseInfos(records []map[string]string) ([]models.NodeInfoRecord, error) {
	result := make([]models.NodeInfoRecord, 0, len(records))
	for idx, record := range records {
		nodeID := firstValue(record, "node_id", "id", "external_id")
		if nodeID == "" {
			return nil, fmt.Errorf("nodes_info record %d missing node_id", idx+1)
		}
		filtered := map[string]string{}
		for key, value := range record {
			filtered[key] = value
		}
		delete(filtered, "node_id")
		delete(filtered, "id")
		delete(filtered, "external_id")
		delete(filtered, "vendor")
		delete(filtered, "model")
		delete(filtered, "description")
		result = append(result, models.NodeInfoRecord{
			NodeExternalID: nodeID,
			Vendor:         firstValue(record, "vendor"),
			Model:          firstValue(record, "model"),
			Description:    firstValue(record, "description"),
			Details:        filtered,
		})
	}
	return result, nil
}

func firstValue(record map[string]string, keys ...string) string {
	for _, key := range keys {
		if value, ok := record[strings.ToLower(key)]; ok {
			return value
		}
	}
	return ""
}

func SortTopologyGroups(groups []models.TopologyGroup) {
	sort.Slice(groups, func(i, j int) bool { return groups[i].Name < groups[j].Name })
}
