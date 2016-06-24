package api

import (
	"fmt"
	"github.com/gorilla/mux"
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"net/http"
	"testing"
	"io"
	"encoding/json"
	"bytes"
	"io/ioutil"
	"time"
)

type SnapshotTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	dt     Dt
	router *mux.Router
}

func (s *SnapshotTestSuit) http(url, method string, body io.Reader) ([]byte, int) {
	// Create a new router for each request
	router := NewRouter(s.dt)
	response, statusCode, err := MakeHTTPRequest(s.T(), router, url, method, body)
	s.assert.NoError(err)
	return response, statusCode
}

func (s *SnapshotTestSuit) SetupTest() {
	s.assert = assertPackage.New(s.T())
	s.dt = Dt{
		Cfg:           &testCfg,
		DtDCOSTools:   &fakeDCOSTools{},
		DtSnapshotJob: &SnapshotJob{},
	}
	s.router = NewRouter(s.dt)
}

func (s *SnapshotTestSuit) TearDownTest() {
	globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
}

func (s *SnapshotTestSuit) TestFindRequestedNodes() {
	mockedGlobalMonitoringResponse := monitoringResponse{
		Nodes: map[string]*Node {
			"10.10.0.1": {
				IP: "10.10.0.1",
				Role: "master",
			},
			"10.10.0.2": {
				IP: "10.10.0.2",
				Host: "my-host.com",
				Role: "master",
			},
			"10.10.0.3": {
				IP: "10.10.0.3",
				MesosID: "12345-12345",
				Role: "master",
			},
			"127.0.0.1": {
				IP: "127.0.0.1",
				Role: "agent",
			},
		},
	}
	globalMonitoringResponse.updateMonitoringResponse(mockedGlobalMonitoringResponse)

	// should return masters + agents
	requestedNodes := []string{"all"}
	nodes, err := findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 4)
	s.assert.Contains(nodes, Node{IP: "10.10.0.1", Role: "master"})
	s.assert.Contains(nodes, Node{IP: "10.10.0.2", Role: "master", Host: "my-host.com"})
	s.assert.Contains(nodes, Node{IP: "10.10.0.3", Role: "master", MesosID: "12345-12345"})
	s.assert.Contains(nodes, Node{IP: "127.0.0.1", Role: "agent"})

	// should return only masters
	requestedNodes = []string{"masters"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 3)
	s.assert.Contains(nodes, Node{IP: "10.10.0.1", Role: "master"})
	s.assert.Contains(nodes, Node{IP: "10.10.0.2", Role: "master", Host: "my-host.com"})
	s.assert.Contains(nodes, Node{IP: "10.10.0.3", Role: "master", MesosID: "12345-12345"})

	// should return only agents
	requestedNodes = []string{"agents"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 1)
	s.assert.Contains(nodes, Node{IP: "127.0.0.1", Role: "agent"})

	// should return host with ip
	requestedNodes = []string{"10.10.0.1"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 1)
	s.assert.Contains(nodes, Node{IP: "10.10.0.1", Role: "master"})

	// should return host with hostname
	requestedNodes = []string{"my-host.com"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 1)
	s.assert.Contains(nodes, Node{IP: "10.10.0.2", Role: "master", Host: "my-host.com"})

	// should return host with mesos-id
	requestedNodes = []string{"12345-12345"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 1)
	s.assert.Contains(nodes, Node{IP: "10.10.0.3", Role: "master", MesosID: "12345-12345"})

	// should return agents and node with ip
	requestedNodes = []string{"agents", "10.10.0.1"}
	nodes, err = findRequestedNodes(requestedNodes, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Len(nodes, 2)
	s.assert.Contains(nodes, Node{IP: "10.10.0.1", Role: "master"})
	s.assert.Contains(nodes, Node{IP: "127.0.0.1", Role: "agent"})
}

func (s *SnapshotTestSuit) TestGetStatus() {
	status := s.dt.DtSnapshotJob.getStatus(s.dt.Cfg)
	s.assert.Equal(status.SnapshotBaseDir, "/tmp/snapshot-test")
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
	st := &fakeDCOSTools{}
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)
	s.dt.DtDCOSTools = st

	status, err := s.dt.DtSnapshotJob.getStatusAll(s.dt.Cfg, s.dt.DtDCOSTools)
	s.assert.Nil(err)
	s.assert.Contains(status, "127.0.0.1")
	s.assert.Equal(status["127.0.0.1"], snapshotReportStatus{
		Running:                               true,
		Status:                                "MyStatus",
		LastSnapshotPath:                      "/path/to/snapshot",
		JobStarted:                            "0001-01-01 00:00:00 +0000 UTC",
		JobEnded:                              "0001-01-01 00:00:00 +0000 UTC",
		JobDuration:                           "2s",
		SnapshotBaseDir:                       "/home/core/1",
		SnapshotJobTimeoutMin:                 720,
		DiskUsedPercent:                       28.0,
		SnapshotUnitsLogsSinceHours:           "24",
		SnapshotJobGetSingleURLTimeoutMinutes: 5,
		CommandExecTimeoutSec:                 10,
	})
}

func (s *SnapshotTestSuit) TestisSnapshotAvailable() {
	url := fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/list", BaseRoute)
	mockedResponse := `[{"file_name": "/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip", "file_size": 123}]`

	st := &fakeDCOSTools{}
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)
	s.dt.DtDCOSTools = st

	// should find
	host, remoteSnapshot, ok, err := s.dt.DtSnapshotJob.isSnapshotAvailable("snapshot-2016-05-13T22:11:36.zip", s.dt.Cfg, s.dt.DtDCOSTools)
	s.assert.True(ok)
	s.assert.Equal(host, "127.0.0.1")
	s.assert.Equal(remoteSnapshot, "/system/health/v1/report/snapshot/serve/snapshot-2016-05-13T22:11:36.zip")
	s.assert.Nil(err)

	// should not find
	host, remoteSnapshot, ok, err = s.dt.DtSnapshotJob.isSnapshotAvailable("snapshot-123.zip", s.dt.Cfg, s.dt.DtDCOSTools)
	s.assert.False(ok)
	s.assert.Empty(host)
	s.assert.Empty(remoteSnapshot)
	s.assert.Nil(err)
}

func (s *SnapshotTestSuit) TestCancelNotRunningJob() {
	url := fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/status", BaseRoute)
	mockedResponse := `
			{
			  "is_running":false,
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
	st := &fakeDCOSTools{}
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)
	s.dt.DtDCOSTools = st

	// Job should fail because it is not running
	response, code := s.http("/system/health/v1/report/snapshot/cancel", "POST", nil)
	s.assert.Equal(code, http.StatusServiceUnavailable)
	var responseJSON snapshotReportResponse
	err := json.Unmarshal(response, &responseJSON)
	s.assert.NoError(err)
	s.assert.Equal(responseJSON, snapshotReportResponse{
		Version: 1,
		Status: "Job is not running",
		ResponseCode: http.StatusServiceUnavailable,
	})
}

// Test we can cancel a job running on a different node.
func (s *SnapshotTestSuit) TestCancelGlobalJob() {

	// mock job status response
	url := "http://127.0.0.1:1050/system/health/v1/report/snapshot/status/all"
	mockedResponse := `{"10.0.7.252":{"is_running":false}}`

	mockedMasters := []Node{
		Node{
			Role: "master",
			IP: "10.0.7.252",
		},
	}

	// add fake response for status/all
	st := &fakeDCOSTools{
		fakeMasters: mockedMasters,
	}
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

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
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)
	s.dt.DtDCOSTools = st

	s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)

	// if we have the url in f.postRequestsMade, that means the redirect worked correctly
	s.assert.Contains(st.postRequestsMade, "http://10.0.7.252:1050/system/health/v1/report/snapshot/cancel")
}

// try cancel a local job
func (s *SnapshotTestSuit) TestCancelLocalJob() {
	s.dt.DtSnapshotJob.Running = true
	s.dt.DtSnapshotJob.cancelChan = make(chan bool, 1)
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/cancel", "POST", nil)
	s.assert.Equal(code, http.StatusOK)

	var responseJSON snapshotReportResponse
	err := json.Unmarshal(response, &responseJSON)
	s.assert.NoError(err)
	s.assert.Equal(responseJSON, snapshotReportResponse{
		Version: 1,
		Status: "Attempting to cancel a job, please check job status.",
		ResponseCode: http.StatusOK,
	})
	r := <- s.dt.DtSnapshotJob.cancelChan
	s.assert.True(r)
}

func (s *SnapshotTestSuit) TestFailRunSnapshotJob() {
	url := fmt.Sprintf("http://127.0.0.1:1050%s/report/snapshot/status", BaseRoute)
	mockedResponse := `
			{
			  "is_running":false,
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
	st := &fakeDCOSTools{}
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)
	s.dt.DtDCOSTools = st

	// should fail since request is in wrong format
	body := bytes.NewBuffer([]byte(`{"nodes": "wrong"}`))
	_, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/create", "POST", body)
	s.assert.Equal(code, http.StatusBadRequest)

	// node should not be found
	body = bytes.NewBuffer([]byte(`{"nodes": ["192.168.0.1"]}`))
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/create", "POST", body)
	s.assert.Equal(code, http.StatusServiceUnavailable)

	var responseJSON snapshotReportResponse
	if err := json.Unmarshal(response, &responseJSON); err != nil {
		s.Assert()
	}
	s.assert.Equal(responseJSON.Status, "Requested nodes: [192.168.0.1] not found")
}

func (s *SnapshotTestSuit) TestRunSnapshot() {
	// add fake response for status/all
	st := &fakeDCOSTools{}

	url := "http://127.0.0.1:1050/system/health/v1/report/snapshot/status"
	mockedResponse := `
			{
			  "is_running":false,
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
	st.makeMockedResponse(url, []byte(mockedResponse), http.StatusOK, nil)

	// update DtDCOSTools
	s.dt.DtDCOSTools = st

	body := bytes.NewBuffer([]byte(`{"nodes": ["all"]}`))
	response, code := s.http("http://127.0.0.1:1050/system/health/v1/report/snapshot/create", "POST", body)
	s.assert.Equal(code, http.StatusOK)
	var responseJSON createResponse
	if err := json.Unmarshal(response, &responseJSON); err != nil {
		s.Assert()
	}
	s.assert.Equal(responseJSON.Status, "Job has been successfully started")
	s.assert.NotEmpty(responseJSON.Extra.LastSnapshotFile)
	time.Sleep(2*time.Second)
	snapshotFiles, err := ioutil.ReadDir("/tmp/snapshot-test")
	s.assert.NoError(err)
	s.assert.True(len(snapshotFiles) > 0)
}

func TestSnapshotTestSuit(t *testing.T) {
	suite.Run(t, new(SnapshotTestSuit))
}
