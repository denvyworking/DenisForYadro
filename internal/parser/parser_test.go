package parser

import (
	"testing"

	"yadrotask/internal/archive"
)

func TestParse(t *testing.T) {
	input := []byte(`
[nodes]
id=node-1 type=host name=host-1 group=compute
id=node-2 type=switch name=switch-1 group=core

[ports]
id=port-1 node_id=node-1 name=eth0 speed=1g peer_node_id=node-2 peer_port_id=port-2
id=port-2 node_id=node-2 name=xe-0/0/1 speed=1g peer_node_id=node-1 peer_port_id=port-1

[nodes_info]
node_id=node-1 vendor=Acme model=H1 description=Primary host
node_id=node-2 vendor=NetCorp model=S1 description=Core switch
`)

	parsed, err := Parse("data/example.zip::log.txt", input)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(parsed.Nodes) != 2 || len(parsed.Ports) != 2 || len(parsed.Infos) != 2 {
		t.Fatalf("unexpected parse counts: %+v", parsed)
	}
}

func TestParseIBDiagnet(t *testing.T) {
	db := []byte(`START_NODES
NodeDesc,NumPorts,NodeType,ClassVersion,BaseVersion,SystemImageGUID,NodeGUID,PortGUID
"HOST_1",1,1,1,1,0xhost1,0xhost1,0xhost1
"SWITCH_1",65,2,1,1,0xswitch1,0xswitch1,0xswitch1
END_NODES

START_PORTS
NodeGuid,PortGuid,PortNum,LinkSpeedActv,LinkSpeedSup
0xhost1,0xhost1,1,4,4
0xswitch1,0xswitch1,1,4,4
END_PORTS

START_SYSTEM_GENERAL_INFORMATION
NodeGuid,SerialNumber,PartNumber,Revision,ProductName
0xswitch1,SOS123,MMM-MAV,AA,"Gorilla"
END_SYSTEM_GENERAL_INFORMATION
`)

	sharp := []byte(`SW_GUID=switch1
endianness = 0
enable_endianness_per_job = 1
`)

	sources := []archive.Source{
		{Path: "data/ibdiagnet2.db_csv", Data: db},
		{Path: "data/ibdiagnet2.sharp_an_info", Data: sharp},
	}

	parsed, err := ParseSources(sources)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(parsed.Nodes) != 2 || len(parsed.Ports) != 2 || len(parsed.Infos) != 2 {
		t.Fatalf("unexpected parse counts: %+v", parsed)
	}
}
