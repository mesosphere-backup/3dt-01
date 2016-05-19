package api

import (
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"net/http"
	"fmt"
)

type FakeHTTPRequest struct {}

func (f *FakeHTTPRequest) Get(url string, timeout time.Duration) ([]byte, int, error) {
	var response string
	if url == fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/status", BaseRoute) {
		response = `
			{
			  "is_running":true,
			  "status":"MyStatus",
			  "errors":null,
			  "last_snapshot_dir":"/path/to/snapshot",
			  "job_started":"0001-01-01 00:00:00 +0000 UTC",
			  "job_ended":"0001-01-01 00:00:00 +0000 UTC",
			  "job_duration":"2s",
			  "snapshot_dir":"/home/core/1",
			  "snapshot_job_timeout_min":720,
			  "snapshot_partition_disk_usage_percent":28.0,
			  "journald_logs_since_hours": "24",
			  "snapshot_job_get_since_url_timeout_min": 5,
			  "command_exec_timeout_sec": 10
			}
		`
	}
	if url == fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/list", BaseRoute) {
		response = `["/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip"]`
	}
	// master
	if url == fmt.Sprintf("http://127.0.0.1:1050%s", BaseRoute) {
		response = `
			{
			  "units": [
			    {
			      "id":"dcos-setup.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Description.",
			      "help":"",
			      "name":"PrettyName"
			    },
			    {
			      "id":"dcos-master.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Master Description.",
			      "help":"",
			      "name":"PrettyName"
			    }
			  ],
			  "hostname":"master01",
			  "ip":"127.0.0.1",
			  "dcos_version":"1.6",
			  "node_role":"master",
			  "mesos_id":"master-123",
			  "3dt_version": "0.0.7"
			}`
	}

	// agent
	if url == fmt.Sprintf("http://127.0.0.2:1050%s", BaseRoute) {
		response = `
			{
			  "units": [
			    {
			      "id":"dcos-setup.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Description.",
			      "help":"",
			      "name":"PrettyName"
			    },
			    {
			      "id":"dcos-agent.service",
			      "health":1,
			      "output":"",
			      "description":"Nice Agent Description.",
			      "help":"",
			      "name":"PrettyName"
			    }
			  ],
			  "hostname":"agent01",
			  "ip":"127.0.0.2",
			  "dcos_version":"1.6",
			  "node_role":"agent",
			  "mesos_id":"agent-123",
			  "3dt_version": "0.0.7"
			}`
	}
	return []byte(response), http.StatusOK, nil
}

func (f *FakeHTTPRequest) Post(url string, timeout time.Duration) (resp []byte, statusCode int, err error) {
	return resp, statusCode, err
}

func (f *FakeHTTPRequest) MakeRequest(req *http.Request, timeout time.Duration) (resp *http.Response, err error) {
	return resp, err
}

type SnapshotTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	dt     Dt
}

func (suit *SnapshotTestSuit) SetupTest() {
	suit.assert = assertPackage.New(suit.T())
	config, _ := LoadDefaultConfig([]string{"3dt", "-snapshot-dir", "/snapshots"})
	suit.dt = Dt{
		Cfg: &config,
		DtHealth: &FakeHealthReport{},
		DtPuller: &FakePuller{},
		DtSnapshotJob: &SnapshotJob{},
		HTTPRequest: &FakeHTTPRequest{},
	}
}

func (suit *SnapshotTestSuit) TearDownTest() {
	GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{})
}

func (s *SnapshotTestSuit) TestFindRequestedNodes() {
	masterNodes := []Node{
		Node{
			Ip: "10.10.0.1",
		},
		Node{
			Host: "my-host.com",
		},
		Node{
			MesosId: "12345-12345",
		},
	}
	agentNodes := []Node{
		Node{
			Ip: "127.0.0.1",
		},
	}
	// should return masters + agents
	requestedNodes := []string{"all"}
	nodes, err := findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, append(masterNodes, agentNodes...))

	// should return only masters
	requestedNodes = []string{"masters"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, masterNodes)

	// should return only agents
	requestedNodes = []string{"agents"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, agentNodes)

	// should return host with ip
	requestedNodes = []string{"10.10.0.1"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, []Node{masterNodes[0]})

	// should return host with hostname
	requestedNodes = []string{"my-host.com"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, []Node{masterNodes[1]})

	// should return host with mesos-id
	requestedNodes = []string{"12345-12345"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, []Node{masterNodes[2]})

	// should return agents and node with ip
	requestedNodes = []string{"agents", "10.10.0.1"}
	nodes, err = findRequestedNodes(masterNodes, agentNodes, requestedNodes)
	s.assert.Nil(err)
	s.assert.Equal(nodes, append(agentNodes, masterNodes[0]))
}

func (s *SnapshotTestSuit) TestGetStatus() {
	status := s.dt.DtSnapshotJob.getStatus(s.dt.Cfg)
	s.assert.Equal(status.SnapshotBaseDir, "/snapshots")
}

func (s *SnapshotTestSuit) TestGetAllStatus() {
	status, err := s.dt.DtSnapshotJob.getStatusAll(s.dt.Cfg, s.dt.DtPuller, s.dt.HTTPRequest)
	s.assert.Nil(err)
	s.assert.Contains(status, "127.0.0.1")
	s.assert.Equal(status["127.0.0.1"], snapshotReportStatus{
		Running: true,
		Status: "MyStatus",
		LastSnapshotPath: "/path/to/snapshot",
		JobStarted: "0001-01-01 00:00:00 +0000 UTC",
		JobEnded: "0001-01-01 00:00:00 +0000 UTC",
		JobDuration: "2s",
		SnapshotBaseDir: "/home/core/1",
		SnapshotJobTimeoutMin: 720,
		DiskUsedPercent: 28.0,
		SnapshotUnitsLogsSinceHours: "24",
		SnapshotJobGetSingleUrlTimeoutMinutes: 5,
		CommandExecTimeoutSec: 10,
	})
}

func (s *SnapshotTestSuit) TestisSnapshotAvailable() {
	// should find
	host, remoteSnapshot, ok, err := s.dt.DtSnapshotJob.isSnapshotAvailable("snapshot-2016-05-13T22:11:36.zip", s.dt.Cfg, s.dt.DtPuller, s.dt.HTTPRequest)
	s.assert.True(ok)
	s.assert.Equal(host, "127.0.0.1")
	s.assert.Equal(remoteSnapshot, "/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip")
	s.assert.Nil(err)

	// should not find
	host, remoteSnapshot, ok, err = s.dt.DtSnapshotJob.isSnapshotAvailable("snapshot-123.zip", s.dt.Cfg, s.dt.DtPuller, s.dt.HTTPRequest)
	s.assert.False(ok)
	s.assert.Empty(host)
	s.assert.Empty(remoteSnapshot)
	s.assert.Nil(err)
}

func TestSnapshotTestSuit(t *testing.T) {
	suite.Run(t, new(SnapshotTestSuit))
}
