package parser

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"yadrotask/internal/archive"
	"yadrotask/internal/models"
)

var sectionHeader = regexp.MustCompile(`^(?:\[\s*([a-zA-Z0-9_\-]+)\s*\]|(?:#\s*)?section\s*:\s*([a-zA-Z0-9_\-]+)|---\s*([a-zA-Z0-9_\-]+)\s*---)$`)

func ParseSources(sources []archive.Source) (models.ParsedLog, error) {
	if len(sources) == 0 {
		return models.ParsedLog{}, fmt.Errorf("no log sources provided")
	}
	if len(sources) == 1 {
		return Parse(sources[0].Path, sources[0].Data)
	}

	var dbSource *archive.Source
	var sharpSource *archive.Source
	for i := range sources {
		lower := strings.ToLower(sources[i].Path)
		switch {
		case strings.Contains(lower, ".db_csv") || looksLikeIBDiagnet(sources[i].Data):
			if dbSource == nil {
				dbSource = &sources[i]
			}
		case strings.Contains(lower, ".sharp_an_info") || looksLikeSharpInfo(sources[i].Data):
			if sharpSource == nil {
				sharpSource = &sources[i]
			}
		}
	}

	if dbSource != nil {
		var sharpData []byte
		if sharpSource != nil {
			sharpData = sharpSource.Data
		}
		return parseIBDiagnet(dbSource.Path, dbSource.Data, sharpData)
	}

	return models.ParsedLog{}, fmt.Errorf("multiple files provided, but no supported ibdiagnet2 log found")
}

func Parse(sourcePath string, data []byte) (models.ParsedLog, error) {
	if looksLikeIBDiagnet(data) {
		return parseIBDiagnet(sourcePath, data, nil)
	}
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

func looksLikeIBDiagnet(data []byte) bool {
	return bytes.Contains(data, []byte("START_NODES")) && bytes.Contains(data, []byte("START_PORTS"))
}

func looksLikeSharpInfo(data []byte) bool {
	return bytes.Contains(data, []byte("SW_GUID="))
}

func parseIBDiagnet(sourcePath string, data []byte, sharpData []byte) (models.ParsedLog, error) {
	sections := splitIBDSections(data)
	nodeLines := sections["nodes"]
	portLines := sections["ports"]
	if len(nodeLines) == 0 {
		return models.ParsedLog{}, fmt.Errorf("ibdiagnet2 nodes section is empty")
	}
	if len(portLines) == 0 {
		return models.ParsedLog{}, fmt.Errorf("ibdiagnet2 ports section is empty")
	}

	nodes, err := parseIBDNodes(nodeLines)
	if err != nil {
		return models.ParsedLog{}, err
	}
	ports, err := parseIBDPorts(portLines)
	if err != nil {
		return models.ParsedLog{}, err
	}

	seenNodes := map[string]struct{}{}
	for _, node := range nodes {
		if _, exists := seenNodes[node.ExternalID]; exists {
			return models.ParsedLog{}, fmt.Errorf("duplicate node id %q", node.ExternalID)
		}
		seenNodes[node.ExternalID] = struct{}{}
	}
	seenPorts := map[string]struct{}{}
	for _, port := range ports {
		if _, exists := seenPorts[port.ExternalID]; exists {
			return models.ParsedLog{}, fmt.Errorf("duplicate port id %q", port.ExternalID)
		}
		seenPorts[port.ExternalID] = struct{}{}
		if _, ok := seenNodes[port.NodeExternalID]; !ok {
			return models.ParsedLog{}, fmt.Errorf("port %q references unknown node %q", port.ExternalID, port.NodeExternalID)
		}
	}
	infos, err := parseIBDInfos(nodes, sections, sharpData)
	if err != nil {
		return models.ParsedLog{}, err
	}

	return models.ParsedLog{SourcePath: sourcePath, Nodes: nodes, Ports: ports, Infos: infos}, nil
}

func splitIBDSections(data []byte) map[string][]string {
	sections := map[string][]string{}
	var current string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "START_") {
			current = strings.ToLower(strings.TrimPrefix(line, "START_"))
			sections[current] = sections[current]
			continue
		}
		if strings.HasPrefix(line, "END_") {
			current = ""
			continue
		}
		if current == "" {
			continue
		}
		sections[current] = append(sections[current], line)
	}
	return sections
}

func parseCSVSection(lines []string) ([]string, [][]string, error) {
	if len(lines) == 0 {
		return nil, nil, fmt.Errorf("empty csv section")
	}
	head, err := parseCSVLine(lines[0])
	if err != nil {
		return nil, nil, fmt.Errorf("parse header: %w", err)
	}
	rows := make([][]string, 0, len(lines)-1)
	for idx := 1; idx < len(lines); idx++ {
		row, err := parseCSVLine(lines[idx])
		if err != nil {
			return nil, nil, fmt.Errorf("parse row %d: %w", idx, err)
		}
		rows = append(rows, row)
	}
	return head, rows, nil
}

func parseCSVLine(line string) ([]string, error) {
	reader := csv.NewReader(strings.NewReader(line))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	return reader.Read()
}

func parseIBDNodes(lines []string) ([]models.NodeRecord, error) {
	head, rows, err := parseCSVSection(lines)
	if err != nil {
		return nil, err
	}
	index := csvIndex(head)
	idxDesc, okDesc := index["nodedesc"]
	idxType, okType := index["nodetype"]
	idxGuid, okGuid := index["nodeguid"]
	if !okDesc || !okType || !okGuid {
		return nil, fmt.Errorf("nodes section missing required columns")
	}

	result := make([]models.NodeRecord, 0, len(rows))
	for _, row := range rows {
		if idxDesc >= len(row) || idxType >= len(row) || idxGuid >= len(row) {
			return nil, fmt.Errorf("nodes row has insufficient columns")
		}
		name := strings.TrimSpace(row[idxDesc])
		typeValue := strings.TrimSpace(row[idxType])
		externalID := strings.TrimSpace(row[idxGuid])
		if name == "" || externalID == "" {
			return nil, fmt.Errorf("nodes row missing id or name")
		}
		nodeType := normalizeNodeType(typeValue)
		group := nodeType + "s"
		result = append(result, models.NodeRecord{ExternalID: externalID, Name: name, Type: nodeType, Group: group})
	}
	return result, nil
}

func parseIBDPorts(lines []string) ([]models.PortRecord, error) {
	head, rows, err := parseCSVSection(lines)
	if err != nil {
		return nil, err
	}
	index := csvIndex(head)
	idxNode, okNode := index["nodeguid"]
	idxPortNum, okPortNum := index["portnum"]
	idxSpeedActv := index["linkspeedactv"]
	idxSpeedSup := index["linkspeedsup"]
	if !okNode || !okPortNum {
		return nil, fmt.Errorf("ports section missing required columns")
	}

	result := make([]models.PortRecord, 0, len(rows))
	for _, row := range rows {
		if idxNode >= len(row) || idxPortNum >= len(row) {
			return nil, fmt.Errorf("ports row has insufficient columns")
		}
		nodeID := strings.TrimSpace(row[idxNode])
		portNum := strings.TrimSpace(row[idxPortNum])
		if nodeID == "" || portNum == "" {
			return nil, fmt.Errorf("ports row missing node or port number")
		}
		externalID := nodeID + ":" + portNum
		speed := ""
		if idxSpeedActv < len(row) {
			speed = strings.TrimSpace(row[idxSpeedActv])
		}
		if speed == "" && idxSpeedSup < len(row) {
			speed = strings.TrimSpace(row[idxSpeedSup])
		}
		result = append(result, models.PortRecord{
			ExternalID:     externalID,
			NodeExternalID: nodeID,
			Name:           "port-" + portNum,
			Speed:          speed,
		})
	}
	return result, nil
}

func parseIBDInfos(nodes []models.NodeRecord, sections map[string][]string, sharpData []byte) ([]models.NodeInfoRecord, error) {
	infoByNode := map[string]*models.NodeInfoRecord{}
	nameToNode := map[string]string{}
	for _, node := range nodes {
		infoByNode[node.ExternalID] = &models.NodeInfoRecord{NodeExternalID: node.ExternalID, Details: map[string]string{}}
		nameToNode[normalizeNodeName(node.Name)] = node.ExternalID
	}

	if lines := sections["system_general_information"]; len(lines) > 0 {
		head, rows, err := parseCSVSection(lines)
		if err != nil {
			return nil, err
		}
		index := csvIndex(head)
		idxNode := index["nodeguid"]
		idxSerial := index["serialnumber"]
		idxPart := index["partnumber"]
		idxRev := index["revision"]
		idxProduct := index["productname"]
		for _, row := range rows {
			if idxNode >= len(row) {
				continue
			}
			nodeID := strings.TrimSpace(row[idxNode])
			info := infoByNode[nodeID]
			if info == nil {
				continue
			}
			if idxPart < len(row) {
				info.Model = strings.TrimSpace(row[idxPart])
			}
			if idxProduct < len(row) {
				info.Description = strings.TrimSpace(row[idxProduct])
			}
			if idxSerial < len(row) {
				info.Details["serial_number"] = strings.TrimSpace(row[idxSerial])
			}
			if idxRev < len(row) {
				info.Details["revision"] = strings.TrimSpace(row[idxRev])
			}
		}
	}

	if lines := sections["switches"]; len(lines) > 0 {
		head, rows, err := parseCSVSection(lines)
		if err != nil {
			return nil, err
		}
		index := csvIndex(head)
		idxNode := index["nodeguid"]
		for _, row := range rows {
			if idxNode >= len(row) {
				continue
			}
			nodeID := strings.TrimSpace(row[idxNode])
			info := infoByNode[nodeID]
			if info == nil {
				continue
			}
			for i, col := range head {
				if i >= len(row) {
					continue
				}
				key := strings.ToLower(col)
				if key == "nodeguid" {
					continue
				}
				info.Details["switch_"+key] = strings.TrimSpace(row[i])
			}
		}
	}

	if len(sharpData) > 0 {
		sharpInfo := parseSharpInfo(sharpData)
		for key, values := range sharpInfo {
			nodeID := key
			if !strings.HasPrefix(strings.ToLower(nodeID), "0x") {
				nodeID = "0x" + nodeID
			}
			info := infoByNode[nodeID]
			if info == nil {
				normalizedName := normalizeNodeName(strings.TrimPrefix(strings.ToLower(nodeID), "0x"))
				if mapped, ok := nameToNode[normalizedName]; ok {
					info = infoByNode[mapped]
				}
			}
			if info == nil {
				continue
			}
			for k, v := range values {
				info.Details[k] = v
			}
		}
	}

	infos := make([]models.NodeInfoRecord, 0, len(infoByNode))
	for _, info := range infoByNode {
		if info.Details == nil {
			info.Details = map[string]string{}
		}
		infos = append(infos, *info)
	}
	return infos, nil
}

func parseSharpInfo(data []byte) map[string]map[string]string {
	result := map[string]map[string]string{}
	var current string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "-") {
			continue
		}
		if strings.HasPrefix(line, "SW_GUID=") {
			current = strings.TrimSpace(strings.TrimPrefix(line, "SW_GUID="))
			if current != "" {
				result[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		result[current]["sharp_"+key] = value
	}
	return result
}

func csvIndex(header []string) map[string]int {
	index := map[string]int{}
	for i, value := range header {
		index[strings.ToLower(strings.TrimSpace(value))] = i
	}
	return index
}

func normalizeNodeType(value string) string {
	switch strings.TrimSpace(value) {
	case "1":
		return "host"
	case "2":
		return "switch"
	default:
		return "node"
	}
}

func normalizeNodeName(name string) string {
	normalized := strings.ToLower(strings.TrimSpace(name))
	normalized = strings.ReplaceAll(normalized, "_", "")
	return normalized
}
