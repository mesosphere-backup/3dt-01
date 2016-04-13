package api

import (
	"time"
	"sync"
)

// top level global variable to store the entire units/nodes status tree
type MonitoringResponse struct {
	sync.RWMutex
	Units map[string]*Unit
	Nodes map[string]*Node
}

// Unit for systemd unit
type Unit struct {
	UnitName  	string
	Nodes     	[]Node
	Health    	int
	Title     	string
	Timestamp 	time.Time
	PrettyName	string
}

// Node for DC/OS node
type Node struct {
	Leader  bool
	Role	string
	Ip	string
	Host	string
	Health	int
	Output	map[string]string
	Units 	[]Unit
	MesosId string
}

// Response received from a remote host
type HttpResponse struct {
	Status int
	Host   string
	Units  []Unit
	Node   Node
}

// responses in JSON format
// units health response used by a local node to send units status
type UnitsHealthResponseJsonStruct struct {
	Array    []UnitHealthResponseFieldsStruct `json:"units"`
	Hostname 	string                    `json:"hostname"`
	IpAddress 	string			  `json:"ip"`
	DcosVersion 	string 			  `json:"dcos_version"`
	Role		string			  `json:"node_role"`
	MesosId		string			  `json:"mesos_id"`
	TdtVersion	string			  `json:"3dt_version"`
}

type UnitHealthResponseFieldsStruct struct {
	UnitId     string `json:"id"`
	UnitHealth int    `json:"health"`
	UnitOutput string `json:"output"`
	UnitTitle  string `json:"description"`
	Help       string `json:"help"`
	PrettyName string `json:"name"`
}

// unit health overview, collected from all hosts
type UnitsResponseJsonStruct struct {
	Array []UnitResponseFieldsStruct `json:"units"`
}

type UnitResponseFieldsStruct struct {
	UnitId     string  `json:"id"`
	PrettyName string  `json:"name"`
	UnitHealth int     `json:"health"`
	UnitTitle  string  `json:"description"`
}

// nodes response
type NodesResponseJsonStruct struct {
	Array []*NodeResponseFieldsStruct `json:"nodes"`
}

type NodeResponseFieldsStruct struct {
	HostIp     string `json:"host_ip"`
	NodeHealth int    `json:"health"`
	NodeRole   string `json:"role"`
}

type NodeResponseFieldsWithErrorStruct struct {
	HostIp     string `json:"host_ip"`
	NodeHealth int    `json:"health"`
	NodeRole   string `json:"role"`
	UnitOutput string `json:"output"`
	Help	   string `json:"help"`
}

// Agent response json format
type AgentsResponse struct {
	Agents []struct {
		Hostname string `json:"hostname"`
	} `json:"slaves"`
}

type ExhibitorNodeResponse struct {
	Code		int
	Description	string
	Hostname	string
	IsLeader	bool
}
