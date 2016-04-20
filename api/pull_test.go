package api

import (
	// intentionally rename package to do some magic
	"fmt"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
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

func (pt *FakePuller) GetUnitsPropertiesViaHttp(url string) ([]byte, int, error) {
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

func (pt *FakePuller) WaitBetweenPulls(interval int) {
}

func (pt *FakePuller) UpdateHttpResponses(responses []*HttpResponse) {
	pt.fakeHttpResponses = responses
}

type PullerTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	puller *FakePuller
}

func (suit *PullerTestSuit) SetupTest() {
	suit.assert = assertPackage.New(suit.T())
	suit.puller = &FakePuller{}
	runPull(1, 1050, suit.puller)
}

func (suit *PullerTestSuit) TearDownTest() {
	GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{})
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

func TestPullerTestSuit(t *testing.T) {
	suite.Run(t, new(PullerTestSuit))
}
