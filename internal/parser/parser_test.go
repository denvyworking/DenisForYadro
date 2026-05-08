package parser

import "testing"

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
