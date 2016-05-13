package api

import (
	// intentionally rename package to do some magic
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Testing SystemdType
type FakeSystemdType struct {
	units []string
}

func (st *FakeSystemdType) GetHostname() string {
	return "MyHostName"
}

func (st *FakeSystemdType) DetectIp() string {
	return "127.0.0.1"
}

func (st *FakeSystemdType) GetNodeRole() string {
	return "master"
}

func (st *FakeSystemdType) GetUnitProperties(pname string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	st.units = append(st.units, pname)
	if pname == "unit_to_fail" {
		return result, errors.New("unit_to_fail occured")
	}
	result["LoadState"] = "loaded"
	result["ActiveState"] = "active"
	result["Description"] = "PrettyName: My fake description"
	return result, nil
}

func (st *FakeSystemdType) InitializeDbusConnection() error {
	return nil
}

func (st *FakeSystemdType) CloseDbusConnection() error {
	return nil
}

func (st *FakeSystemdType) GetUnitNames() (units []string, err error) {
	units = []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service", "unit_a", "unit_b", "unit_c", "unit_to_fail"}
	return units, err
}

func (st *FakeSystemdType) GetJournalOutput(unit string) (string, error) {
	return "journal output", nil
}

func (st *FakeSystemdType) GetMesosNodeId(role string, field string) string {
	return "node-id-123"
}

type HandlersTestSuit struct {
	suite.Suite
	assert                              *assertPackage.Assertions
	router                              *mux.Router
	cfg                                 Config
	mockedUnitsHealthResponseJsonStruct UnitsHealthResponseJsonStruct
	mockedMonitoringResponse            MonitoringResponse
}

// SetUp/Teardown
func (suit *HandlersTestSuit) SetupTest() {
	// setup variables
	args := []string{"3dt", "test"}
	suit.cfg, _ = LoadDefaultConfig(args)
	suit.cfg.Systemd = &FakeSystemdType{}
	suit.router = NewRouter(&suit.cfg)
	suit.assert = assertPackage.New(suit.T())

	// mock the response
	suit.mockedUnitsHealthResponseJsonStruct = UnitsHealthResponseJsonStruct{
		Array: []UnitHealthResponseFieldsStruct{
			{
				UnitId:     "dcos-master.service",
				UnitHealth: 0,
				UnitTitle:  "Master service",
				PrettyName: "DC/OS Master service unit",
			},
			{
				UnitId:     "dcos-ddt.service",
				UnitHealth: 0,
				UnitTitle:  "Diag service",
				PrettyName: "3dt",
			},
		},
		Hostname:    "localhost",
		IpAddress:   "127.0.0.1",
		DcosVersion: "1.7-dev",
		Role:        "master",
		MesosId:     "12345",
		TdtVersion:  "1.2.3",
	}
	suit.mockedMonitoringResponse = MonitoringResponse{
		Units: map[string]*Unit{
			"dcos-adminrouter-reload.service": &Unit{
				UnitName: "dcos-adminrouter-reload.service",
				Nodes: []Node{
					{
						Role:   "master",
						Ip:     "10.0.7.190",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
					},
					{
						Role:   "agent",
						Ip:     "10.0.7.191",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S1",
					},
				},
				Health:     0,
				Title:      "Reload admin router to get new DNS",
				Timestamp:  time.Now(),
				PrettyName: "Admin Router Reload",
			},
			"dcos-cosmos.service": &Unit{
				UnitName: "dcos-cosmos.service",
				Nodes: []Node{
					{
						Role:   "agent",
						Ip:     "10.0.7.192",
						Host:   "",
						Health: 1,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-cosmos.service":             "Some nasty error occured",
						},
						MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S2",
					},
					{
						Role:   "agent",
						Ip:     "10.0.7.193",
						Host:   "",
						Health: 0,
						Output: map[string]string{
							"dcos-adminrouter-reload.service": "",
							"dcos-adminrouter-reload.timer":   "",
						},
						MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S3",
					},
				},
				Health:     1,
				Title:      "DCOS Packaging API",
				Timestamp:  time.Now(),
				PrettyName: "Package Service",
			},
		},
		Nodes: map[string]*Node{
			"10.0.7.190": &Node{
				Role:   "master",
				Ip:     "10.0.7.190",
				Health: 0,
				Output: map[string]string{
					"dcos-adminrouter-reload.service": "",
					"dcos-adminrouter-reload.timer":   "",
				},
				Units: []Unit{
					{
						UnitName: "dcos-adminrouter-reload.service",
						Nodes: []Node{
							{
								Role:   "master",
								Ip:     "10.0.7.190",
								Host:   "",
								Health: 0,
								Output: map[string]string{
									"dcos-adminrouter-reload.service": "",
									"dcos-adminrouter-reload.timer":   "",
								},
								MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
							},
							{
								Role:   "agent",
								Ip:     "10.0.7.191",
								Host:   "",
								Health: 0,
								Output: map[string]string{
									"dcos-adminrouter-reload.service": "",
									"dcos-adminrouter-reload.timer":   "",
								},
								MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S1",
							},
						},
						Health:     0,
						Title:      "Reload admin router to get new DNS",
						Timestamp:  time.Now(),
						PrettyName: "Admin Router Reload",
					},
				},
				MesosId: "ab098f2a-799c-4d85-82b2-eb5159d0ceb0",
			},
		},
	}

	// Update global monitoring responses
	GlobalMonitoringResponse.UpdateMonitoringResponse(suit.mockedMonitoringResponse)
	HealthReport.UpdateHealthReport(suit.mockedUnitsHealthResponseJsonStruct)
}

func (suit *HandlersTestSuit) TearDownTest() {
	// clear global variables that might be set
	HealthReport = UnitsHealth{}
	GlobalMonitoringResponse = MonitoringResponse{}
}

// Helper functions
func MakeHttpRequest(t *testing.T, router *mux.Router, url string) (response []byte, err error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return response, err
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != 200 {
		return response, errors.New(fmt.Sprintf("wrong HTTP response: %d", w.Code))
	}
	return w.Body.Bytes(), nil
}

func (s *HandlersTestSuit) get(url string) []byte {
	response, err := MakeHttpRequest(s.T(), s.router, url)
	s.assert.Nil(err, "Error makeing GET request")
	return response
}

// Tests
func (s *HandlersTestSuit) TestUnitsHealthStruct() {
	// Test structure HealthReport get/set health report
	HealthReport.UpdateHealthReport(UnitsHealthResponseJsonStruct{})
	s.assert.Equal(HealthReport.GetHealthReport(), UnitsHealthResponseJsonStruct{}, "GetHealthReport() should be empty")
	HealthReport.UpdateHealthReport(s.mockedUnitsHealthResponseJsonStruct)
	s.assert.Equal(HealthReport.GetHealthReport(), s.mockedUnitsHealthResponseJsonStruct, "GetHealthReport() should NOT be empty")
}

func (s *HandlersTestSuit) TestUnitsHealthStatusFunc() {
	// Test health endpoint /system/health/v1
	resp := s.get("/system/health/v1")
	var response UnitsHealthResponseJsonStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, UnitsHealthResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Equal(response, s.mockedUnitsHealthResponseJsonStruct)
}

func (s *HandlersTestSuit) TestgetAllUnitsHandlerFunc() {
	// Test endpoint /system/health/v1/units
	resp := s.get("/system/health/v1/units")

	var response UnitsResponseJsonStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, UnitsResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 2, "Expected 2 units in response")
	s.assert.Contains(response.Array, UnitResponseFieldsStruct{
		UnitId:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})
	s.assert.Contains(response.Array, UnitResponseFieldsStruct{
		UnitId:     "dcos-cosmos.service",
		PrettyName: "Package Service",
		UnitHealth: 1,
		UnitTitle:  "DCOS Packaging API",
	})
}

func (s *HandlersTestSuit) TestgetUnitByIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unit>
	resp := s.get("/system/health/v1/units/dcos-cosmos.service")

	var response UnitResponseFieldsStruct
	json.Unmarshal(resp, &response)

	expectedResponse := UnitResponseFieldsStruct{
		UnitId:     "dcos-cosmos.service",
		PrettyName: "Package Service",
		UnitHealth: 1,
		UnitTitle:  "DCOS Packaging API",
	}
	s.assert.NotEqual(response, UnitsResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Equal(response, expectedResponse, "Response is in incorrect format")

	// Unit should not be found
	resp = s.get("/system/health/v1/units/dcos-notfound.service")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestgetNodesByUnitIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unit>/nodes
	resp := s.get("/system/health/v1/units/dcos-cosmos.service/nodes")
	var response NodesResponseJsonStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, NodesResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 2, "Number of hosts must be 2")

	s.assert.Contains(response.Array, &NodeResponseFieldsStruct{
		HostIp:     "10.0.7.192",
		NodeHealth: 1,
		NodeRole:   "agent",
	})
	s.assert.Contains(response.Array, &NodeResponseFieldsStruct{
		HostIp:     "10.0.7.193",
		NodeHealth: 0,
		NodeRole:   "agent",
	})

	// Unit should not be found and no nodes should be returned
	resp = s.get("/system/health/v1/units/dcos-notfound.service/nodes")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestgetNodeByUnitIdNodeIdHandlerFunc() {
	// Test endpoint /system/health/v1/units/<unitid>/nodes/<nodeid>
	resp := s.get("/system/health/v1/units/dcos-cosmos.service/nodes/10.0.7.192")

	var response NodeResponseFieldsWithErrorStruct
	json.Unmarshal(resp, &response)
	s.assert.NotEqual(response, NodeResponseFieldsWithErrorStruct{}, "Response should not be empty")

	expectedResponse := NodeResponseFieldsWithErrorStruct{
		HostIp:     "10.0.7.192",
		NodeHealth: 1,
		NodeRole:   "agent",
		UnitOutput: "Some nasty error occured",
		Help:       "Node available at `dcos node ssh -mesos-id ab098f2a-799c-4d85-82b2-eb5159d0ceb0-S2`. Try, `journalctl -xv` to diagnose further.",
	}
	s.assert.Equal(response, expectedResponse, "Response is in incorrect format")

	// use wrong unit
	resp = s.get("/system/health/v1/units/dcos-notfound.service/nodes/10.0.7.192")
	s.assert.Equal(string(resp), "{}\n")

	// use wrong node
	resp = s.get("/system/health/v1/units/dcos-cosmos.service/nodes/127.0.0.1")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestgetNodesHandlerFunc() {
	// Test endpoint /system/health/v1/nodes
	resp := s.get("/system/health/v1/nodes")

	var response NodesResponseJsonStruct
	json.Unmarshal(resp, &response)

	s.assert.NotEqual(response, NodesResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 1, "Number of nodes in respons must be 1")
	fmt.Printf("%s\n", response.Array)
	s.assert.Contains(response.Array, &NodeResponseFieldsStruct{
		HostIp:     "10.0.7.190",
		NodeHealth: 0,
		NodeRole:   "master",
	})
}

func (s *HandlersTestSuit) TestgetNodeByIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>
	resp := s.get("/system/health/v1/nodes/10.0.7.190")

	var response NodeResponseFieldsStruct
	json.Unmarshal(resp, &response)

	s.assert.Equal(response, NodeResponseFieldsStruct{
		HostIp:     "10.0.7.190",
		NodeHealth: 0,
		NodeRole:   "master",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestgetNodeUnitsByNodeIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>/units
	resp := s.get("/system/health/v1/nodes/10.0.7.190/units")

	var response UnitsResponseJsonStruct
	json.Unmarshal(resp, &response)
	s.assert.NotEqual(response, UnitsResponseJsonStruct{}, "Response cannot be empty")
	s.assert.Len(response.Array, 1, "Response should have 1 unit")
	s.assert.Contains(response.Array, UnitResponseFieldsStruct{
		UnitId:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1/units")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestgetNodeUnitByNodeIdUnitIdHandlerFunc() {
	// Test endpoint /system/health/v1/nodes/<nodeid>/units/<unitid>
	resp := s.get("/system/health/v1/nodes/10.0.7.190/units/dcos-adminrouter-reload.service")

	var response UnitResponseFieldsStruct
	json.Unmarshal(resp, &response)
	s.assert.Equal(response, UnitResponseFieldsStruct{
		UnitId:     "dcos-adminrouter-reload.service",
		PrettyName: "Admin Router Reload",
		UnitHealth: 0,
		UnitTitle:  "Reload admin router to get new DNS",
	})

	// use wrong host
	resp = s.get("/system/health/v1/nodes/127.0.0.1/units/dcos-adminrouter-reload.service")
	s.assert.Equal(string(resp), "{}\n")

	// use wrong service
	resp = s.get("/system/health/v1/nodes/10.0.7.190/units/dcos-bad.service")
	s.assert.Equal(string(resp), "{}\n")
}

func (s *HandlersTestSuit) TestreportHandlerFunc() {
	// Test endpoint /system/health/v1/report
	resp := s.get("/system/health/v1/report")

	var response MonitoringResponse
	json.Unmarshal(resp, &response)
	s.assert.Len(response.Units, 2)
	s.assert.Len(response.Nodes, 1)
}

func (s *HandlersTestSuit) TestIsInListFunc() {
	array := []string{"DC", "OS", "SYS"}
	s.assert.Equal(IsInList("DC", array), true, "DC should be in test array")
	s.assert.Equal(IsInList("CD", array), false, "CD should not be in test array")

}

func (s *HandlersTestSuit) TestStartUpdateHealthReportActualImplementationFunc() {
	// clear any health report
	HealthReport.UpdateHealthReport(UnitsHealthResponseJsonStruct{})
	s.cfg.Systemd = &SystemdType{}

	readyChan := make(chan bool, 1)
	StartUpdateHealthReport(s.cfg, readyChan, true)
	hr := HealthReport.GetHealthReport()
	s.assert.Equal(hr, UnitsHealthResponseJsonStruct{})
}

// TestCheckHealthReportRace is meant to be run under the race detector
// to confirm that UpdateHealthReport and GetHealthReport do not race.
func (s *HandlersTestSuit) TestCheckHealthReportRace() {
	// the strategy we follow is to launch a goroutine that
	// updates the HealthReport while reading it
	// from the main thread.
	done := make(chan struct{})
	go func() {
		HealthReport.UpdateHealthReport(UnitsHealthResponseJsonStruct{})
		close(done)
	}()
	_ = HealthReport.GetHealthReport()
	// We wait for the spawned goroutine to exit before the test
	// returns in order to prevent the spawned goroutine from racing
	// with TearDownTest.
	<-done
}

func (s *HandlersTestSuit) TestStartUpdateHealthReportFunc() {
	readyChan := make(chan bool, 1)
	StartUpdateHealthReport(s.cfg, readyChan, true)
	hr := HealthReport.GetHealthReport()
	s.assert.Equal(hr, UnitsHealthResponseJsonStruct{
		Array: []UnitHealthResponseFieldsStruct{
			{
				UnitId:     "unit_a",
				UnitHealth: 0,
				UnitTitle:  "My fake description",
				PrettyName: "PrettyName",
			},
			{
				UnitId:     "unit_b",
				UnitHealth: 0,
				UnitTitle:  "My fake description",
				PrettyName: "PrettyName",
			},
			{
				UnitId:     "unit_c",
				UnitHealth: 0,
				UnitTitle:  "My fake description",
				PrettyName: "PrettyName",
			},
		},
		Hostname:    "MyHostName",
		IpAddress:   "127.0.0.1",
		DcosVersion: "",
		Role:        "master",
		MesosId:     "node-id-123",
		TdtVersion:  "0.0.13",
	})
}

func TestHandlersTestSuit(t *testing.T) {
	suite.Run(t, new(HandlersTestSuit))
}
