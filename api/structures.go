package api

import (
	"sync"
	"time"

	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
)

// MonitoringResponse top level global variable to store the entire units/nodes status tree.
type MonitoringResponse struct {
	sync.Mutex

	Units       map[string]Unit
	Nodes       map[string]Node
	UpdatedTime time.Time
}

// Unit for stands for systemd unit.
type Unit struct {
	UnitName   string
	Nodes      []Node `json:",omitempty"`
	Health     int
	Title      string
	Timestamp  time.Time
	PrettyName string
}

// Node for DC/OS node.
type Node struct {
	Leader  bool
	Role    string
	IP      string
	Host    string
	Health  int
	Output  map[string]string
	Units   []Unit `json:",omitempty"`
	MesosID string
}

// httpResponse a structure of http response from a remote host.
type httpResponse struct {
	Status int
	Units  []Unit
	Node   Node
}

// UnitsHealthResponseJSONStruct json response /system/health/v1
type UnitsHealthResponseJSONStruct struct {
	Array       []HealthResponseValues `json:"units"`
	System      sysMetrics             `json:"system"`
	Hostname    string                 `json:"hostname"`
	IPAddress   string                 `json:"ip"`
	DcosVersion string                 `json:"dcos_version"`
	Role        string                 `json:"node_role"`
	MesosID     string                 `json:"mesos_id"`
	TdtVersion  string                 `json:"3dt_version"`
}

// HealthResponseValues is a health values json response.
type HealthResponseValues struct {
	UnitID     string `json:"id"`
	UnitHealth int    `json:"health"`
	UnitOutput string `json:"output"`
	UnitTitle  string `json:"description"`
	Help       string `json:"help"`
	PrettyName string `json:"name"`
}

type sysMetrics struct {
	Memory      mem.VirtualMemoryStat `json:"memory"`
	LoadAvarage load.AvgStat          `json:"load_avarage"`
	Partitions  []disk.PartitionStat  `json:"partitions"`
	DiskUsage   []disk.UsageStat      `json:"disk_usage"`
}

// UnitsResponseJSONStruct contains health overview, collected from all hosts
type UnitsResponseJSONStruct struct {
	Array []UnitResponseFieldsStruct `json:"units"`
}

// UnitResponseFieldsStruct contains systemd unit health report.
type UnitResponseFieldsStruct struct {
	UnitID     string `json:"id"`
	PrettyName string `json:"name"`
	UnitHealth int    `json:"health"`
	UnitTitle  string `json:"description"`
}

// NodesResponseJSONStruct contains an array of responses from nodes.
type NodesResponseJSONStruct struct {
	Array []*NodeResponseFieldsStruct `json:"nodes"`
}

// NodeResponseFieldsStruct contains a response from a node.
type NodeResponseFieldsStruct struct {
	HostIP     string `json:"host_ip"`
	NodeHealth int    `json:"health"`
	NodeRole   string `json:"role"`
}

// NodeResponseFieldsWithErrorStruct contains node response with errors.
type NodeResponseFieldsWithErrorStruct struct {
	HostIP     string `json:"host_ip"`
	NodeHealth int    `json:"health"`
	NodeRole   string `json:"role"`
	UnitOutput string `json:"output"`
	Help       string `json:"help"`
}

// Agent response json format
type agentsResponse struct {
	Agents []struct {
		Hostname   string `json:"hostname"`
		Attributes struct {
			PublicIP string `json:"public_ip"`
		} `json:"attributes"`
	} `json:"slaves"`
}

type exhibitorNodeResponse struct {
	Code        int
	Description string
	Hostname    string
	IsLeader    bool
}

// Dt is a struct of dependencies used in 3dt code. There are 2 implementations, the one runs on a real system and
// the one used for testing.
type Dt struct {
	Cfg               *Config
	DtDCOSTools       DCOSHelper
	DtDiagnosticsJob  *DiagnosticsJob
	RunPullerChan     chan bool
	RunPullerDoneChan chan bool
	SystemdUnits      *SystemdUnits
	MR                *MonitoringResponse
}

type bundle struct {
	File string `json:"file_name"`
	Size int64  `json:"file_size"`
}

// UnitPropertiesResponse is a structure to unmarshal dbus.GetunitProperties response
type UnitPropertiesResponse struct {
	ID             string `json:"Id"`
	LoadState      string
	ActiveState    string
	SubState       string
	Description    string
	ExecMainStatus int

	InactiveExitTimestampMonotonic  uint64
	ActiveEnterTimestampMonotonic   uint64
	ActiveExitTimestampMonotonic    uint64
	InactiveEnterTimestampMonotonic uint64
}
