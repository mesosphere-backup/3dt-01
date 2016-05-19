package api

import (
	// intentionally rename package to do some magic
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"sync"
)

// Fake Interface and implementation for Pulling functionality
type FakePuller struct {
	test              bool
	urls              []string
	fakeHttpResponses []*HttpResponse
}

func (pt *FakePuller) GetTimestamp() time.Time {
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func (pt *FakePuller) LookupMaster() (nodes []Node, err error) {
	var fakeMasterHost Node
	fakeMasterHost.Ip = "127.0.0.1"
	fakeMasterHost.Role = "master"
	nodes = append(nodes, fakeMasterHost)
	return nodes, nil
}

func (pt *FakePuller) GetAgentsFromMaster() (nodes []Node, err error) {
	var fakeAgentHost Node
	fakeAgentHost.Ip = "127.0.0.2"
	fakeAgentHost.Role = "agent"
	nodes = append(nodes, fakeAgentHost)
	return nodes, nil
}

//func (pt *FakePuller) GetHttp(url string) ([]byte, int, error) {
//	var response string
//
//	if url == fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/status", BaseRoute) {
//		response = `
//			{
//			  "is_running":true,
//			  "status":"MyStatus",
//			  "errors":null,
//			  "last_snapshot_dir":"/path/to/snapshot",
//			  "job_started":"0001-01-01 00:00:00 +0000 UTC",
//			  "job_ended":"0001-01-01 00:00:00 +0000 UTC",
//			  "job_duration":"2s",
//			  "snapshot_dir":"/home/core/1",
//			  "snapshot_job_timeout_min":720,
//			  "snapshot_partition_disk_usage_percent":28.0,
//			  "journald_logs_since_hours": "24",
//			  "snapshot_job_get_since_url_timeout_min": 5,
//			  "command_exec_timeout_sec": 10
//			}
//		`
//	}
//	if url == fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/list", BaseRoute) {
//		response = `["/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip"]`
//	}
//	// master
//	if url == fmt.Sprintf("http://127.0.0.1:1050%s", BaseRoute) {
//		response = `
//			{
//			  "units": [
//			    {
//			      "id":"dcos-setup.service",
//			      "health":0,
//			      "output":"",
//			      "description":"Nice Description.",
//			      "help":"",
//			      "name":"PrettyName"
//			    },
//			    {
//			      "id":"dcos-master.service",
//			      "health":0,
//			      "output":"",
//			      "description":"Nice Master Description.",
//			      "help":"",
//			      "name":"PrettyName"
//			    }
//			  ],
//			  "hostname":"master01",
//			  "ip":"127.0.0.1",
//			  "dcos_version":"1.6",
//			  "node_role":"master",
//			  "mesos_id":"master-123",
//			  "3dt_version": "0.0.7"
//			}`
//	}
//
//	// agent
//	if url == fmt.Sprintf("http://127.0.0.2:1050%s", BaseRoute) {
//		response = `
//			{
//			  "units": [
//			    {
//			      "id":"dcos-setup.service",
//			      "health":0,
//			      "output":"",
//			      "description":"Nice Description.",
//			      "help":"",
//			      "name":"PrettyName"
//			    },
//			    {
//			      "id":"dcos-agent.service",
//			      "health":1,
//			      "output":"",
//			      "description":"Nice Agent Description.",
//			      "help":"",
//			      "name":"PrettyName"
//			    }
//			  ],
//			  "hostname":"agent01",
//			  "ip":"127.0.0.2",
//			  "dcos_version":"1.6",
//			  "node_role":"agent",
//			  "mesos_id":"agent-123",
//			  "3dt_version": "0.0.7"
//			}`
//	}
//	return []byte(response), 200, nil
//}

func (pt *FakePuller) WaitBetweenPulls(interval int) {
}

func (pt *FakePuller) UpdateHttpResponses(responses []*HttpResponse) {
	pt.fakeHttpResponses = responses
}

type PullerTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	dt     Dt
}

func (suit *PullerTestSuit) SetupTest() {
	suit.assert = assertPackage.New(suit.T())
	suit.dt = Dt{
		DtPuller: &FakePuller{},
		HTTPRequest: &FakeHTTPRequest{},
	}
	runPull(1, 1050, suit.dt)
}

func (suit *PullerTestSuit) TearDownTest() {
	GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{})
}

// TestMonitoringResponseRace checks that the various exported methods
// of the MonitoringResponse don't race. It does so by calling the methods
// concurrently and will fail under the race detector if the methods race.
func (s *PullerTestSuit) TestMonitoringResponseRace() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{})
	}()
	// We call UpdateMonitoringResponse twice to ensure the RWMutex's write lock
	// is held, not just a read lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetAllUnits()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetNodesForUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetSpecificNodeForUnit("node-ip", "test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetNodes()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetNodeById("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetNodeUnitsId("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		GlobalMonitoringResponse.GetNodeUnitByNodeIdUnitId("test-ip", "test-unit")
	}()
	wg.Wait()
}

func (s *PullerTestSuit) TestPullerFindUnit() {
	// dcos-master.service should be in monitoring responses
	unit, err := GlobalMonitoringResponse.GetUnit("dcos-master.service")
	s.assert.Nil(err)
	s.assert.Equal(unit, UnitResponseFieldsStruct{
		"dcos-master.service",
		"PrettyName",
		0,
		"Nice Master Description.",
	})
}

func (s *PullerTestSuit) TestPullerNotFindUnit() {
	// dcos-service-not-here.service should not be in responses
	unit, err := GlobalMonitoringResponse.GetUnit("dcos-service-not-here.service")
	s.assert.Error(err)
	s.assert.Equal(unit, UnitResponseFieldsStruct{})
}

func (s *PullerTestSuit) TestdnsRespondergetAgentSource() {
	dns := dnsResponder{
		defaultMasterAddress: "leader-non-existed.mesos",
	}
	ips, err := dns.getAgentSource()
	s.assert.Empty(ips)
	s.assert.NotEmpty(err)

	dns2 := dnsResponder{
		defaultMasterAddress: "dcos.io",
	}
	ips2, err := dns2.getAgentSource()
	s.assert.NotEmpty(ips2)
	s.assert.Empty(err)
}

func (s *PullerTestSuit) TestdnsRespondergetMesosAgents() {
	dns := dnsResponder{
		defaultMasterAddress: "leader-non-existed.mesos",
	}
	nodes, err := dns.getMesosAgents([]string{"127.0.0.1"})
	s.assert.Empty(nodes)
	s.assert.NotEmpty(err)
}

func (s *PullerTestSuit) TesthistoryServiceRespondergetAgentSource() {
	h := historyServiceResponder{
		defaultPastTime: "/hour/",
	}
	files, err := h.getAgentSource()
	s.assert.Empty(files)
	s.assert.NotEmpty(err)
}

func (s *PullerTestSuit) TesthistoryServiceRespondergetMesosAgents() {
	h := historyServiceResponder{
		defaultPastTime: "/hour/",
	}
	nodes, err := h.getMesosAgents([]string{"/tmp/test.json"})
	s.assert.Empty(nodes)
	s.assert.NotEmpty(err)
}

func TestPullerTestSuit(t *testing.T) {
	suite.Run(t, new(PullerTestSuit))
}
