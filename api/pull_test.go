package api

import (
	// intentionally rename package to do some magic
	"fmt"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"sync"
	"testing"
	"time"
)

// Fake Interface and implementation for Pulling functionality
type fakePuller struct {
	test              bool
	urls              []string
	fakeHTTPResponses []*httpResponse
}

func (pt *fakePuller) GetTimestamp() time.Time {
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func (pt *fakePuller) LookupMaster() (nodes []Node, err error) {
	var fakeMasterHost Node
	fakeMasterHost.IP = "127.0.0.1"
	fakeMasterHost.Role = "master"
	nodes = append(nodes, fakeMasterHost)
	return nodes, nil
}

func (pt *fakePuller) GetAgentsFromMaster() (nodes []Node, err error) {
	var fakeAgentHost Node
	fakeAgentHost.IP = "127.0.0.2"
	fakeAgentHost.Role = "agent"
	nodes = append(nodes, fakeAgentHost)
	return nodes, nil
}

func (pt *fakePuller) GetUnitsPropertiesViaHTTP(url string) ([]byte, int, error) {
	var response string

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
	return []byte(response), 200, nil
}

func (pt *fakePuller) WaitBetweenPulls(interval int) {
}

func (pt *fakePuller) UpdateHTTPResponses(responses []*httpResponse) {
	pt.fakeHTTPResponses = responses
}

type PullerTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	puller *fakePuller
}

func (s *PullerTestSuit) SetupTest() {
	s.assert = assertPackage.New(s.T())
	s.puller = &fakePuller{}
	runPull(1, 1050, s.puller)
}

func (s *PullerTestSuit) TearDownTest() {
	globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
}

// TestMonitoringResponseRace checks that the various exported methods
// of the MonitoringResponse don't race. It does so by calling the methods
// concurrently and will fail under the race detector if the methods race.
func (s *PullerTestSuit) TestMonitoringResponseRace() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
	}()
	// We call UpdateMonitoringResponse twice to ensure the RWMutex's write lock
	// is held, not just a read lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetAllUnits()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodesForUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetSpecificNodeForUnit("node-ip", "test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodes()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeByID("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeUnitsID("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeUnitByNodeIDUnitID("test-ip", "test-unit")
	}()
	wg.Wait()
}

func (s *PullerTestSuit) TestPullerFindUnit() {
	// dcos-master.service should be in monitoring responses
	unit, err := globalMonitoringResponse.GetUnit("dcos-master.service")
	s.assert.Nil(err)
	s.assert.Equal(unit, unitResponseFieldsStruct{
		"dcos-master.service",
		"PrettyName",
		0,
		"Nice Master Description.",
	})
}

func (s *PullerTestSuit) TestPullerNotFindUnit() {
	// dcos-service-not-here.service should not be in responses
	unit, err := globalMonitoringResponse.GetUnit("dcos-service-not-here.service")
	s.assert.Error(err)
	s.assert.Equal(unit, unitResponseFieldsStruct{})
}

func TestPullerTestSuit(t *testing.T) {
	suite.Run(t, new(PullerTestSuit))
}
