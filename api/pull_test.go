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
	mockedNode        Node
}

func (pt *FakePuller) GetTimestamp() time.Time {
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func (pt *FakePuller) LookupMaster() (nodes []Node, err error) {
	if pt.mockedNode.Ip != "" {
		return []Node{pt.mockedNode}, nil
	}
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
