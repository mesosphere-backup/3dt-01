package api

import (
	// intentionally rename package to do some magic
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/mux"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var testCfg Config

func init() {
	testCfg, _ = LoadDefaultConfig([]string{"3dt", "-role", "master", "-diagnostics-bundle-dir", "/tmp/snapshot-test"})
}

// fakeDCOSTools is a DCOSHelper interface implementation used for testing.
type fakeDCOSTools struct {
	sync.Mutex
	units             []string
	fakeHTTPResponses []*httpResponse
	fakeMasters       []Node

	// HTTP GET, POST
	mockedRequest    map[string]FakeHTTPContainer
	getRequestsMade  []string
	postRequestsMade []string
	rawRequestsMade  []*http.Request
}

type FakeHTTPContainer struct {
	mockResponse   []byte
	mockStatusCode int
	mockErr        error
}

func (st *fakeDCOSTools) makeMockedResponse(url string, response []byte, statusCode int, e error) error {
	if _, ok := st.mockedRequest[url]; ok {
		return errors.New(url + " is already added")
	}
	st.mockedRequest = make(map[string]FakeHTTPContainer)
	st.mockedRequest[url] = FakeHTTPContainer{
		mockResponse:   response,
		mockStatusCode: statusCode,
		mockErr:        e,
	}
	return nil
}

func (st *fakeDCOSTools) GetHostname() (string, error) {
	return "MyHostName", nil
}

func (st *fakeDCOSTools) DetectIP() (string, error) {
	return "127.0.0.1", nil
}

func (st *fakeDCOSTools) GetNodeRole() (string, error) {
	return "master", nil
}

func (st *fakeDCOSTools) GetUnitProperties(pname string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	st.units = append(st.units, pname)
	if pname == "unit_to_fail" {
		return result, errors.New("unit_to_fail occured")
	}
	result["Id"] = pname
	result["LoadState"] = "loaded"
	result["ActiveState"] = "active"
	result["Description"] = "PrettyName: My fake description"
	result["SubState"] = "running"
	return result, nil
}

func (st *fakeDCOSTools) InitializeDBUSConnection() error {
	return nil
}

func (st *fakeDCOSTools) CloseDBUSConnection() error {
	return nil
}

func (st *fakeDCOSTools) GetUnitNames() (units []string, err error) {
	units = []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service", "unit_a", "unit_b", "unit_c", "unit_to_fail"}
	return units, err
}

func (st *fakeDCOSTools) GetJournalOutput(unit string) (string, error) {
	return "journal output", nil
}

func (st *fakeDCOSTools) GetMesosNodeID() (string, error) {
	return "node-id-123", nil
}

// Make HTTP GET request with a timeout.
func (st *fakeDCOSTools) Get(url string, timeout time.Duration) (body []byte, statusCode int, err error) {
	st.Lock()
	defer st.Unlock()
	// add made GET request.
	st.getRequestsMade = append(st.getRequestsMade, url)

	if _, ok := st.mockedRequest[url]; ok {
		return st.mockedRequest[url].mockResponse, st.mockedRequest[url].mockStatusCode, st.mockedRequest[url].mockErr
	}
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

// Post make HTTP POST request with a timeout.
func (st *fakeDCOSTools) Post(url string, timeout time.Duration) (body []byte, statusCode int, err error) {
	st.Lock()
	defer st.Unlock()
	st.postRequestsMade = append(st.postRequestsMade, url)
	return body, statusCode, nil
}

// MakeRequest makes a HTTP request
func (st *fakeDCOSTools) HTTPRequest(req *http.Request, timeout time.Duration) (resp *http.Response, err error) {
	st.Lock()
	defer st.Unlock()
	st.rawRequestsMade = append(st.rawRequestsMade, req)
	return resp, nil
}

func (st *fakeDCOSTools) UpdateHTTPResponses(responses []*httpResponse) {
	st.fakeHTTPResponses = responses
}

func (st *fakeDCOSTools) GetTimestamp() time.Time {
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func (st *fakeDCOSTools) GetMasterNodes() (nodes []Node, err error) {
	if len(st.fakeMasters) > 0 {
		return st.fakeMasters, nil
	}
	var fakeMasterHost Node
	fakeMasterHost.IP = "127.0.0.1"
	fakeMasterHost.Role = "master"
	nodes = append(nodes, fakeMasterHost)
	return nodes, nil
}

func (st *fakeDCOSTools) GetAgentNodes() (nodes []Node, err error) {
	var fakeAgentHost Node
	fakeAgentHost.IP = "127.0.0.2"
	fakeAgentHost.Role = "agent"
	nodes = append(nodes, fakeAgentHost)
	return nodes, nil
}

type HandlersTestSuit struct {
	suite.Suite
	assert                              *assertPackage.Assertions
	router                              *mux.Router
	dt                                  Dt
	mockedUnitsHealthResponseJSONStruct UnitsHealthResponseJSONStruct
	mockedMonitoringResponse            monitoringResponse
}

// SetUp/Teardown
func (s *HandlersTestSuit) SetupTest() {
	// setup variables
	flagSet = flag.NewFlagSet("3dt", flag.ContinueOnError)
	s.dt = Dt{
		Cfg:         &testCfg,
		DtDCOSTools: &fakeDCOSTools{},
	}
	s.router = NewRouter(s.dt)
	s.assert = assertPackage.New(s.T())

	// mock the response
	s.mockedUnitsHealthResponseJSONStruct = UnitsHealthResponseJSONStruct{
		Array: []healthResponseValues{
			{
				UnitID:     "dcos-master.service",
				UnitHealth: 0,
				UnitTitle:  "Master service",
				PrettyName: "DC/OS Master service unit",
			},
			{
				UnitID:     "dcos-ddt.service",
				UnitHealth: 0,
				UnitTitle:  "Diag service",
				PrettyName: "3dt",
			},
		},
		Hostname:    "localhost",
		IPAddress:   "127.0.0.1",
		DcosVersion: "1.7-dev",
		Role:        "master",
		MesosID:     "12345",
		TdtVersion:  "1.2.3",
	}
	s.mockedMonitoringResponse = monitoringResponse{
		Units: map[string]unit{
			"dcos-adminrouter-reload.service": unit{
				UnitName: "dcos-adminrouter-reload.service",
				Nodes: []Node{
					{
						Role:   "master",
						IP:     "10.0.7.190",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
					},
					{
						Role:   "agent",
						IP:     "10.0.7.191",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S1",
					},
				},
				Health:     0,
				Title:      "Reload admin router to get new DNS",
				Timestamp:  time.Now(),
				PrettyName: "Admin Router Reload",
			},
			"dcos-cosmos.service": unit{
				UnitName: "dcos-cosmos.service",
				Nodes: []Node{
					{
						Role:   "agent",
						IP:     "10.0.7.192",
						Host:   "",
						Health: 1,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-cosmos.service":             "Some nasty error occured",
						},
						MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S2",
					},
					{
						Role:   "agent",
						IP:     "10.0.7.193",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S3",
					},
				},
				Health:     1,
				Title:      "DCOS Packaging API",
				Timestamp:  time.Now(),
				PrettyName: "Package Service",
			},
		},
		Nodes: map[string]Node{
			"10.0.7.190": Node{
				Role:   "master",
				IP:     "10.0.7.190",
				Health: 0,
				Output: map[string]string{
					"dcos-adminrouter-reload.service": "",
					"dcos-adminrouter-reload.timer":   "",
				},
				Units: []unit{
					{
						UnitName: "dcos-adminrouter-reload.service",
						Nodes: []Node{
							{
								Role:   "master",
								IP:     "10.0.7.190",
								Host:   "",
								Health: 0,
								Output: map[string]string{
									"dcos-adminrouter-reload.service": "",
									"dcos-adminrouter-reload.timer":   "",
								},
								MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
							},
							{
								Role:   "agent",
								IP:     "10.0.7.191",
								Host:   "",
								Health: 0,
								Output: map[string]string{
									"dcos-adminrouter-reload.service": "",
									"dcos-adminrouter-reload.timer":   "",
								},
								MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S1",
							},
						},
						Health:     0,
						Title:      "Reload admin router to get new DNS",
						Timestamp:  time.Now(),
						PrettyName: "Admin Router Reload",
					},
				},
				MesosID: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
			},
		},
	}

	// Update global monitoring responses
	globalMonitoringResponse.updateMonitoringResponse(&s.mockedMonitoringResponse)
}

func (s *HandlersTestSuit) TearDownTest() {
	// clear global variables that might be set
	globalMonitoringResponse = monitoringResponse{}
}

// Helper functions
func MakeHTTPRequest(t *testing.T, router *mux.Router, url, method string, body io.Reader) (response []byte, statusCode int, err error) {
	req, err := http.NewRequest(method, url, body)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w.Body.Bytes(), w.Code, err
}

func (s *HandlersTestSuit) get(url string) []byte {
	response, _, err := MakeHTTPRequest(s.T(), s.router, url, "GET", nil)
	s.assert.Nil(err, "Error makeing GET request")
	return response
}

func (s *HandlersTestSuit) TestgetAllUnitsHandlerFunc() {
	// Test endpoint /system/health/v1/units
	resp := s.get("/system/health/v1/units")

	var response unitsResponseJSONStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, unitsResponseJSONStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 2, "Expected 2 units in response")
	s.assert.Contains(response.Array, unitResponseFieldsStruct{
		UnitID:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})
	s.assert.Contains(response.Array, unitResponseFieldsStruct{
		UnitID:     "dcos-cosmos.service",
		PrettyName: "Package Service",
		UnitHealth: 1,
		UnitTitle:  "DCOS Packaging API",
	})
}

func (s *HandlersTestSuit) TestgetUnitByIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unit>
	resp := s.get("/system/health/v1/units/dcos-cosmos.service")

	var response unitResponseFieldsStruct
	json.Unmarshal(resp, &response)

	expectedResponse := unitResponseFieldsStruct{
		UnitID:     "dcos-cosmos.service",
		PrettyName: "Package Service",
		UnitHealth: 1,
		UnitTitle:  "DCOS Packaging API",
	}
	s.assert.NotEqual(response, unitsResponseJSONStruct{}, "Response cannot be empty")
	s.assert.Equal(response, expectedResponse, "Response is in incorrect format")

	// Unit should not be found
	resp = s.get("/system/health/v1/units/dcos-notfound.service")
	s.assert.Equal(string(resp), "Unit dcos-notfound.service not found\n")
}

func (s *HandlersTestSuit) TestgetNodesByUnitIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unit>/nodes
	resp := s.get("/system/health/v1/units/dcos-cosmos.service/nodes")
	var response nodesResponseJSONStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, nodesResponseJSONStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 2, "Number of hosts must be 2")

	s.assert.Contains(response.Array, &nodeResponseFieldsStruct{
		HostIP:     "10.0.7.192",
		NodeHealth: 1,
		NodeRole:   "agent",
	})
	s.assert.Contains(response.Array, &nodeResponseFieldsStruct{
		HostIP:     "10.0.7.193",
		NodeHealth: 0,
		NodeRole:   "agent",
	})

	// Unit should not be found and no nodes should be returned
	resp = s.get("/system/health/v1/units/dcos-notfound.service/nodes")
	s.assert.Equal(string(resp), "Unit dcos-notfound.service not found\n")
}

func (s *HandlersTestSuit) TestgetNodeByUnitIdNodeIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unitid>/nodes/<nodeid>
	resp := s.get("/system/health/v1/units/dcos-cosmos.service/nodes/10.0.7.192")

	var response nodeResponseFieldsWithErrorStruct
	json.Unmarshal(resp, &response)
	s.assert.NotEqual(response, nodeResponseFieldsWithErrorStruct{}, "Response should not be empty")

	expectedResponse := nodeResponseFieldsWithErrorStruct{
		HostIP:     "10.0.7.192",
		NodeHealth: 1,
		NodeRole:   "agent",
		UnitOutput: "Some nasty error occured",
		Help:       "Node available at `dcos node ssh -mesos-id ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S2`. Try, `journalctl -xv` to diagnose further.",
	}
	s.assert.Equal(response, expectedResponse, "Response is in incorrect format")

	// use wrong unit
	resp = s.get("/system/health/v1/units/dcos-notfound.service/nodes/10.0.7.192")
	s.assert.Equal(string(resp), "Unit dcos-notfound.service not found\n")

	// use wrong node
	resp = s.get("/system/health/v1/units/dcos-cosmos.service/nodes/127.0.0.1")
	s.assert.Equal(string(resp), "Node 127.0.0.1 not found\n")
}

func (s *HandlersTestSuit) TestgetNodesHandlerFunc() {
	// Test endpoint /system/health/v1/nodes
	resp := s.get("/system/health/v1/nodes")

	var response nodesResponseJSONStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, nodesResponseJSONStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 1, "Number of nodes in respons must be 1")
	s.assert.Contains(response.Array, &nodeResponseFieldsStruct{
		HostIP:     "10.0.7.190",
		NodeHealth: 0,
		NodeRole:   "master",
	})
}

func (s *HandlersTestSuit) TestgetNodeByIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>
	resp := s.get("/system/health/v1/nodes/10.0.7.190")

	var response nodeResponseFieldsStruct
	json.Unmarshal(resp, &response)

	s.assert.Equal(response, nodeResponseFieldsStruct{
		HostIP:     "10.0.7.190",
		NodeHealth: 0,
		NodeRole:   "master",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1")
	s.assert.Equal(string(resp), "Node 127.0.0.1 not found\n")
}

func (s *HandlersTestSuit) TestgetNodeUnitsByNodeIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>/units
	resp := s.get("/system/health/v1/nodes/10.0.7.190/units")

	var response unitsResponseJSONStruct
	json.Unmarshal(resp, &response)
	s.assert.NotEqual(response, unitsResponseJSONStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 1, "Response should have 1 unit")
	s.assert.Contains(response.Array, unitResponseFieldsStruct{
		UnitID:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1/units")
	s.assert.Equal(string(resp), "Node 127.0.0.1 not found\n")
}

func (s *HandlersTestSuit) TestgetNodeUnitByNodeIdUnitIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>/units/<unitid>
	resp := s.get("/system/health/v1/nodes/10.0.7.190/units/dcos-adminrouter-reload.service")

	var response unitResponseFieldsStruct
	json.Unmarshal(resp, &response)
	s.assert.Equal(response, unitResponseFieldsStruct{
		UnitID:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1/units/dcos-adminrouter-reload.service")
	s.assert.Equal(string(resp), "Node 127.0.0.1 not found\n")

	// use wrong service
	resp = s.get("/system/health/v1/nodes/10.0.7.190/units/dcos-bad.service")
	s.assert.Equal(string(resp), "Unit dcos-bad.service not found\n")
}

func (s *HandlersTestSuit) TestreportHandlerFunc() {
	// Test endpoint /system/health/v1/report
	resp := s.get("/system/health/v1/report")

	var response monitoringResponse
	json.Unmarshal(resp, &response)
	s.assert.Len(response.Units, 2)
	s.assert.Len(response.Nodes, 1)
}

func (s *HandlersTestSuit) TestIsInListFunc() {
	array := []string{"DC", "OS", "SYS"}
	s.assert.Equal(isInList("DC", array), true, "DC should be in test array")
	s.assert.Equal(isInList("CD", array), false, "CD should not be in test array")

}

func TestHandlersTestSuit(t *testing.T) {
	suite.Run(t, new(HandlersTestSuit))
}
