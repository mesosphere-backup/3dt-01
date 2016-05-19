package api

import (
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"testing"
	"time"
	"net/http"
	"fmt"
	"errors"
	"github.com/gorilla/mux"
	log "github.com/Sirupsen/logrus"
	"sync"
	"encoding/json"
	"io"
	"bytes"
)

type FakeHTTPRequest struct {
	sync.Mutex
	mockedRequest map[string]FakeHTTPContainer
	getRequestsMade []string
	postRequestsMade []string
	rawRequestsMade []*http.Request
}

type FakeHTTPContainer struct {
	mockResponse   []byte
	mockStatusCode int
	mockErr        error
}

func (f *FakeHTTPRequest) makeMockedResponse(url string, response []byte, statusCode int, e error) error {
	if _, ok := f.mockedRequest[url]; ok {
		return errors.New(url+" is already added")
	}
	f.mockedRequest = make(map[string]FakeHTTPContainer)
	f.mockedRequest[url] = FakeHTTPContainer{
		mockResponse: response,
		mockStatusCode: statusCode,
		mockErr: e,
	}
	return nil
}

func (f *FakeHTTPRequest) Get(url string, timeout time.Duration) (body []byte, statusCode int, err error) {
	// make race detector happy
	f.Lock()
	defer f.Unlock()

	// track all get requests made
	f.getRequestsMade = append(f.getRequestsMade, url)
	if _, ok := f.mockedRequest[url]; ok {
		return f.mockedRequest[url].mockResponse, f.mockedRequest[url].mockStatusCode, f.mockedRequest[url].mockErr
	}

	// master
	if url == fmt.Sprintf("http://127.0.0.1:1050%s", BaseRoute) {
		response := `
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
		return []byte(response), http.StatusOK, nil
	}

	// agent
	if url == fmt.Sprintf("http://127.0.0.2:1050%s", BaseRoute) {
		response := `
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
		return []byte(response), http.StatusOK, nil
	}
	return body, http.StatusBadRequest, errors.New(url+" not found")
}

func (f *FakeHTTPRequest) Post(url string, timeout time.Duration) (resp []byte, statusCode int, err error) {
	f.Lock()
	defer f.Unlock()

	// track all post requests made
	f.postRequestsMade = append(f.postRequestsMade, url)
	return resp, statusCode, err
}

func (f *FakeHTTPRequest) MakeRequest(req *http.Request, timeout time.Duration) (resp *http.Response, err error) {
	f.Lock()
	defer f.Unlock()

	// track all other requests made
	f.rawRequestsMade = append(f.rawRequestsMade, req)
	return resp, err
}

type SnapshotTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	dt     Dt
	router *mux.Router
}

func (s *SnapshotTestSuit) http(url, method string, body io.Reader) ([]byte, int) {
	// Create a new router for each request
	router := NewRouter(s.dt)
	response, statusCode, err := MakeFakeHttpRequest(s.T(), router, url, method, body)
	if err != nil {
		log.Error(err)
	}
	return response, statusCode
}

func (suit *SnapshotTestSuit) SetupTest() {
	suit.assert = assertPackage.New(suit.T())
	config, _ := LoadDefaultConfig([]string{"3dt", "-snapshot-dir", "/tmp/snapshots"})
	suit.dt = Dt{
		Cfg: &config,
		DtHealth: &FakeHealthReport{},
		DtPuller: &FakePuller{},
		DtSnapshotJob: &SnapshotJob{},
		HTTPRequest: &FakeHTTPRequest{},
	}
	suit.router = NewRouter(suit.dt)
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
	s.assert.Equal(status.SnapshotBaseDir, "/tmp/snapshots")
}

func (s *SnapshotTestSuit) TestGetAllStatus() {
	url := fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/status", BaseRoute)
	mockedResponse := `
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
	f := &FakeHTTPRequest{}
	f.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

	status, err := s.dt.DtSnapshotJob.getStatusAll(s.dt.Cfg, s.dt.DtPuller, f)
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
	url := fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/list", BaseRoute)
	mockedResponse := `["/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip"]`
	f := &FakeHTTPRequest{}
	f.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

	// should find
	host, remoteSnapshot, ok, err := s.dt.DtSnapshotJob.isSnapshotAvailable("snapshot-2016-05-13T22:11:36.zip", s.dt.Cfg, s.dt.DtPuller, f)
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

func (s *SnapshotTestSuit) TestCancelNotRunningJob() {
	// Job should fail because it is not running
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)
	s.assert.Equal(code, http.StatusServiceUnavailable)
	var responseJson snapshotReportResponse
	err := json.Unmarshal(response, &responseJson)
	s.assert.NoError(err)
	s.assert.Equal(responseJson, snapshotReportResponse{
		Version: 1,
		Status: "Job is not running",
		ResponseCode: http.StatusServiceUnavailable,
	})
}

// Should fail because cancellation on agent nodes is not allowed.
func (s *SnapshotTestSuit) TestCancelAgentRole() {
	// should fail because agent role is not supported
	f := &FakeHealthReport{}
	f.customRole = "agent"
	s.dt.DtHealth = f
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)
	s.assert.Equal(code, http.StatusServiceUnavailable)

	var responseJson snapshotReportResponse
	err := json.Unmarshal(response, &responseJson)
	s.assert.NoError(err)
	s.assert.Equal(responseJson, snapshotReportResponse{
		Version: 1,
		Status: "Canceling snapshot job on agent node is not implemented.",
		ResponseCode: http.StatusServiceUnavailable,
	})
}

// Test we can cancel a job running on a different node.
func (s *SnapshotTestSuit) TestCancelGlobalJob() {

	// mock job status response
	url := "http://127.0.0.1:1050/system/health/v1/report/snapshot/status/all"
	mockedResponse := `
		{"10.0.7.252":{"is_running":false,"status":"","errors":null,"last_snapshot_dir":"","job_started":"0001-01-01 00:00:00 +0000 UTC","job_ended":"0001-01-01 00:00:00 +0000 UTC","job_duration":"0","snapshot_dir":"/opt/mesosphere/snapshots","snapshot_job_timeout_min":720,"journald_logs_since_hours":"24","snapshot_job_get_since_url_timeout_min":5,"command_exec_timeout_sec":10,"snapshot_partition_disk_usage_percent":0}}
	`
	// add fake response for status/all
	f := &FakeHTTPRequest{}
	f.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

	// add fake response for status 10.0.7.252
	url = "http://10.0.7.252:1050/system/health/v1/report/snapshot/status"
	mockedResponse = `
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
	f.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

	p := &FakePuller{}
	p.mockedNode = Node{
		Role: "master",
		Ip: "10.0.7.252",
	}

	s.dt.HTTPRequest = f
	s.dt.DtPuller = p
	s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)

	// if we have the url in f.postRequestsMade, that means the redirect worked correctly
	s.assert.Contains(f.postRequestsMade, "http://10.0.7.252:1050/system/health/v1/report/snapshot/cancel")
}

// try cancel a local job
func (s *SnapshotTestSuit) TestCancelLocalJob() {
	s.dt.DtSnapshotJob.Running = true
	s.dt.DtSnapshotJob.cancelChan = make(chan bool, 1)
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)
	s.assert.Equal(code, http.StatusOK)

	var responseJson snapshotReportResponse
	err := json.Unmarshal(response, &responseJson)
	s.assert.NoError(err)
	s.assert.Equal(responseJson, snapshotReportResponse{
		Version: 1,
		Status: "Attempting to cancel a job, please check job status.",
		ResponseCode: http.StatusOK,
	})
	r := <- s.dt.DtSnapshotJob.cancelChan
	s.assert.True(r)
}

func (s *SnapshotTestSuit) TestFailRunSnapshotJob() {
	// should fail since request is in wrong format
	body := bytes.NewBuffer([]byte(`{"nodes": "wrong"}`))
	_, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/create", "POST", body)
	s.assert.Equal(code, http.StatusBadRequest)

	//
	body = bytes.NewBuffer([]byte(`{"nodes": ["192.168.0.1"]}`))
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/create", "POST", body)
	s.assert.Equal(code, http.StatusServiceUnavailable)

	var responseJson snapshotReportResponse
	if err := json.Unmarshal(response, &responseJson); err != nil {
		s.Assert()
	}
	s.assert.Equal(responseJson.Status, "Requested nodes: [192.168.0.1] not found")
}

func TestSnapshotTestSuit(t *testing.T) {
	suite.Run(t, new(SnapshotTestSuit))
}
