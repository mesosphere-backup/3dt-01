package api

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/shirou/gopsutil/disk"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SnapshotJob a main structure for a logs collection job.
type SnapshotJob struct {
	sync.Mutex
	cancelChan chan bool

	Running          bool          `json:"is_running"`
	Status           string        `json:"status"`
	Errors           []string      `json:"errors"`
	LastSnapshotPath string        `json:"last_snapshot_dir"`
	JobStarted       time.Time     `json:"job_started"`
	JobEnded         time.Time     `json:"job_ended"`
	JobDuration      time.Duration `json:"job_duration"`
}

// snapshot job response format
type snapshotReportResponse struct {
	ResponseCode int      `json:"response_http_code"`
	Version      int      `json:"version"`
	Status       string   `json:"status"`
	Errors       []string `json:"errors"`
}

// snapshot job status format
type snapshotReportStatus struct {
	// job related fields
	Running          bool     `json:"is_running"`
	Status           string   `json:"status"`
	Errors           []string `json:"errors"`
	LastSnapshotPath string   `json:"last_snapshot_dir"`
	JobStarted       string   `json:"job_started"`
	JobEnded         string   `json:"job_ended"`
	JobDuration      string   `json:"job_duration"`

	// config related fields
	SnapshotBaseDir                       string `json:"snapshot_dir"`
	SnapshotJobTimeoutMin                 int    `json:"snapshot_job_timeout_min"`
	SnapshotUnitsLogsSinceHours           string `json:"journald_logs_since_hours"`
	SnapshotJobGetSingleURLTimeoutMinutes int    `json:"snapshot_job_get_since_url_timeout_min"`
	CommandExecTimeoutSec                 int    `json:"command_exec_timeout_sec"`

	// metrics related
	DiskUsedPercent float64 `json:"snapshot_partition_disk_usage_percent"`
}

// Create snapshot request structure, example:   {"nodes": ["all"]}
type snapshotCreateRequest struct {
	Version int
	Nodes   []string
}

// start a snapshot job
func (j *SnapshotJob) run(req snapshotCreateRequest, config *Config, DCOSTools DCOSHelper) (response snapshotReportResponse, err error) {
	role, err := DCOSTools.GetNodeRole()
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}

	if role == "agent" {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("Running snapshot job on agent node is not implemented."))
	}

	isRunning, _, err := j.isRunning(config, DCOSTools)
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	if isRunning {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("Job is already running"))
	}

	_, err = os.Stat(config.FlagSnapshotDir)
	if os.IsNotExist(err) {
		log.Infof("snapshot dir: %s not found, attempting to create one", config.FlagSnapshotDir)
		if err := os.Mkdir(config.FlagSnapshotDir, os.ModePerm); err != nil {
			j.Status = "Could not create snapshot directory: " + config.FlagSnapshotDir
			return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New(j.Status))
		}
	}

	// Null errors on every new run.
	j.Errors = nil

	t := time.Now()
	j.LastSnapshotPath = fmt.Sprintf("%s/snapshot-%d-%02d-%02dT%02d:%02d:%02d-%d.zip", config.FlagSnapshotDir, t.Year(),
		t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
	j.Status = "Snapshot job started, archive will be available: " + j.LastSnapshotPath

	// first discover all nodes in a cluster, then try to find requested nodes.
	masterNodes, err := DCOSTools.GetMasterNodes()
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	agentNodes, err := DCOSTools.GetAgentNodes()
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	foundNodes, err := findRequestedNodes(masterNodes, agentNodes, req.Nodes)
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	log.Debugf("Found requested nodes: %s", foundNodes)

	j.cancelChan = make(chan bool)
	go j.runBackgroundJob(foundNodes, config, DCOSTools)
	return prepareResponseOk(http.StatusOK, "Snapshot job started: "+filepath.Base(j.LastSnapshotPath))
}

//
func (j *SnapshotJob) runBackgroundJob(nodes []Node, config *Config, DCOSTools DCOSHelper) {
	log.Info("Started background job")
	// log a start time
	j.JobStarted = time.Now()

	// log end time
	defer func(j *SnapshotJob) {
		j.JobEnded = time.Now()
		j.JobDuration = time.Since(j.JobStarted)
		log.Info("Job finished")
	}(j)

	// lets start a goroutine which will timeout background report job after a certain time.
	jobIsDone := make(chan bool)
	go func(jobIsDone chan bool, j *SnapshotJob) {
		select {
		case <-jobIsDone:
			return
		case <-time.After(time.Minute * time.Duration(config.FlagSnapshotJobTimeoutMinutes)):
			j.Status = "Job failed"
			errMsg := fmt.Sprintf("snapshot job timedout after: %s", time.Since(j.JobStarted))
			j.Errors = append(j.Errors, errMsg)
			log.Error(errMsg)
			j.cancelChan <- true
			return
		}
	}(jobIsDone, j)

	// makesure we always cancel a timeout goroutine when the report is finished.
	defer func(jobIsDone chan bool) {
		jobIsDone <- true
	}(jobIsDone)

	// Update job running field.
	j.start()
	defer j.stop()

	// create a zip file
	zipfile, err := os.Create(j.LastSnapshotPath)
	if err != nil {
		j.Status = "Job failed"
		errMsg := fmt.Sprintf("Coult not create zip file: %s", j.LastSnapshotPath)
		j.Errors = append(j.Errors, errMsg)
		log.Error(errMsg)
		return
	}
	defer zipfile.Close()

	zipWriter := zip.NewWriter(zipfile)
	defer zipWriter.Close()

	// place a summaryErrorsReport.txt in a zip archive which should provide info what failed during the logs collection.
	summaryErrorsReport := new(bytes.Buffer)
	defer func(zipWriter *zip.Writer, summaryReport *bytes.Buffer) {
		zipFile, err := zipWriter.Create("summaryErrorsReport.txt")
		if err != nil {
			j.Status = "Could not append a summaryErrorsReport.txt to a zip file, node"
			log.Error(j.Status)
			log.Error(err)
			j.Errors = append(j.Errors, err.Error())
			return
		}
		io.Copy(zipFile, summaryReport)
	}(zipWriter, summaryErrorsReport)

	// lock out reportJob staructure
	j.Lock()
	defer j.Unlock()

	for _, node := range nodes {
		url := fmt.Sprintf("http://%s:%d%s/logs", node.IP, config.FlagPort, BaseRoute)
		select {
		case _, ok := <-j.cancelChan:
			if ok {
				j.Status = "Job has been canceled"
				j.Errors = append(j.Errors, j.Status)
				log.Debug(j.Status)
				os.Remove(zipfile.Name())
				j.LastSnapshotPath = ""
				return
			}
			errMsg := "cancelChan is closed!"
			j.Errors = append(j.Errors, errMsg)
			return

		default:
			j.Status = fmt.Sprintf("Collecting from a node: %s, url: %s", node.IP, url)
			log.Debug(j.Status)
		}

		endpoints := make(map[string]string)
		body, statusCode, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err != nil {
			errMsg := fmt.Sprintf("could not get a list of logs, url: %s, status code %d", url, statusCode)
			j.Errors = append(j.Errors, errMsg)
			log.Error(err)
			updateSummaryReport(errMsg, node, err.Error(), summaryErrorsReport)
			continue
		}
		if err = json.Unmarshal(body, &endpoints); err != nil {
			errMsg := "could not unmarshal a list of logs, url: " + url
			j.Errors = append(j.Errors, errMsg)
			log.Error(err)
			updateSummaryReport(errMsg, node, err.Error(), summaryErrorsReport)
			continue
		}

		// add http endpoints
		err = j.getHTTPAddToZip(node, endpoints, j.LastSnapshotPath, zipWriter, summaryErrorsReport, config, DCOSTools)
		if err != nil {
			errMsg := "could not add logs for a node, url: " + url
			j.Errors = append(j.Errors, errMsg)
			log.Error(err)
			updateSummaryReport(errMsg, node, err.Error(), summaryErrorsReport)
		}
	}
	if len(j.Errors) == 0 {
		j.Status = "Snapshot job sucessfully finished"
	}
}

// delete a snapshot
func (j *SnapshotJob) delete(snapshotName string, config *Config, DCOSTools DCOSHelper) (response snapshotReportResponse, err error) {
	if !strings.HasPrefix(snapshotName, "snapshot-") || !strings.HasSuffix(snapshotName, ".zip") {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("format allowed  snapshot-*.zip"))
	}

	j.Lock()
	defer j.Unlock()

	// first try to locate a snapshot on a local disk.
	snapshotPath := path.Join(config.FlagSnapshotDir, snapshotName)
	log.Debugf("Trying remove snapshot: %s", snapshotPath)
	_, err = os.Stat(snapshotPath)
	if err == nil {
		if err = os.Remove(snapshotPath); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		msg := "Deleted " + snapshotPath
		log.Infof(msg)
		return prepareResponseOk(http.StatusOK, msg)
	}

	node, _, ok, err := j.isSnapshotAvailable(snapshotName, config, DCOSTools)
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	if ok {
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/delete/%s", node, config.FlagPort, BaseRoute, snapshotName)
		j.Status = "Attempting to delete a snapshot on a remote host. POST " + url
		log.Debug(j.Status)
		timeout := time.Duration(time.Second * 5)
		response, _, err := DCOSTools.Post(url, timeout)
		if err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		// unmarshal a response from a remote node and return it back.
		var remoteResponse snapshotReportResponse
		if err = json.Unmarshal(response, &remoteResponse); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		j.Status = remoteResponse.Status
		return remoteResponse, nil
	}
	j.Status = "Snapshot not found " + snapshotName
	return prepareResponseOk(http.StatusNotFound, j.Status)
}

// isRunning returns if the snapshot job is running, node the job is running on and error. If the node is empty
// string, then the job is running on a localhost.
func (j *SnapshotJob) isRunning(config *Config, DCOSTools DCOSHelper) (bool, string, error) {
	// first check if the job is running on a localhost.
	if j.Running {
		return true, "", nil
	}

	// try to discover if the job is running on other masters.
	clusterSnapshotStatus, err := j.getStatusAll(config, DCOSTools)
	if err != nil {
		return false, "", err
	}
	for node, status := range clusterSnapshotStatus {
		if status.Running == true {
			return true, node, nil
		}
	}

	// no running job found.
	return false, "", nil
}

// Collect all status reports from master nodes and return a map[master_ip] snapshotReportStatus
// The function is used to get a job status on other nodes
func (j *SnapshotJob) getStatusAll(config *Config, DCOSTools DCOSHelper) (map[string]snapshotReportStatus, error) {
	statuses := make(map[string]snapshotReportStatus)

	masterNodes, err := DCOSTools.GetMasterNodes()
	if err != nil {
		return statuses, err
	}

	for _, master := range masterNodes {
		var status snapshotReportStatus
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/status", master.IP, config.FlagPort, BaseRoute)
		body, _, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err = json.Unmarshal(body, &status); err != nil {
			log.Error(err)
			continue
		}
		statuses[master.IP] = status
	}
	if len(statuses) == 0 {
		return statuses, errors.New("Could not determine wheather the snapshot job is running or not.")
	}
	return statuses, nil
}

// get a status report for a localhost
func (j *SnapshotJob) getStatus(config *Config) snapshotReportStatus {
	// use a temp var `used`, since disk.Usage panics if partition does not exist.
	var used float64
	usageStat, err := disk.Usage(config.FlagSnapshotDir)
	if err == nil {
		used = usageStat.UsedPercent
	} else {
		log.Errorf("Could not get a disk usage: %s", config.FlagSnapshotDir)
	}
	return snapshotReportStatus{
		Running:          j.Running,
		Status:           j.Status,
		Errors:           j.Errors,
		LastSnapshotPath: j.LastSnapshotPath,
		JobStarted:       j.JobStarted.String(),
		JobEnded:         j.JobEnded.String(),
		JobDuration:      j.JobDuration.String(),

		SnapshotBaseDir:                       config.FlagSnapshotDir,
		SnapshotJobTimeoutMin:                 config.FlagSnapshotJobTimeoutMinutes,
		SnapshotJobGetSingleURLTimeoutMinutes: config.FlagSnapshotJobGetSingleURLTimeoutMinutes,
		SnapshotUnitsLogsSinceHours:           config.FlagSnapshotUnitsLogsSinceHours,
		CommandExecTimeoutSec:                 config.FlagCommandExecTimeoutSec,

		DiskUsedPercent: used,
	}
}

// fetch an HTTP endpoint and append the output to a zip file.
func (j *SnapshotJob) getHTTPAddToZip(node Node, endpoints map[string]string, folder string, zipWriter *zip.Writer,
	summaryReport *bytes.Buffer, config *Config, DCOSTools DCOSHelper) error {
	for fileName, httpEndpoint := range endpoints {
		fullURL := "http://" + node.IP + httpEndpoint
		j.Status = "GET " + fullURL
		log.Debug(j.Status)
		timeout := time.Duration(time.Minute * time.Duration(config.FlagSnapshotJobGetSingleURLTimeoutMinutes))
		request, err := http.NewRequest("GET", fullURL, nil)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			log.Error(err)
			updateSummaryReport(fmt.Sprintf("could not create request for url: %s", fullURL), node, err.Error(), summaryReport)
			continue
		}
		request.Header.Add("Accept-Encoding", "gzip")
		resp, err := Requester.Do(request, timeout)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			log.Errorf("Could not fetch url: %s", fullURL)
			log.Error(err)
			updateSummaryReport(fmt.Sprintf("could not fetch url: %s", fullURL), node, err.Error(), summaryReport)
			continue
		}
		if resp.Header.Get("Content-Encoding") == "gzip" {
			fileName += ".gz"
		}

		// put all logs in a `ip_role` folder
		zipFile, err := zipWriter.Create(filepath.Join(node.IP+"_"+node.Role, fileName))
		if err != nil {
			resp.Body.Close()
			j.Errors = append(j.Errors, err.Error())
			log.Errorf("Could not add %s to a zip archive", fileName)
			log.Error(err)
			updateSummaryReport(fmt.Sprintf("could not add a file %s to a zip", fileName), node, err.Error(), summaryReport)
			continue
		}
		io.Copy(zipFile, resp.Body)
		resp.Body.Close()
	}
	return nil
}

func prepareResponseOk(httpStatusCode int, okMsg string) (response snapshotReportResponse, err error) {
	response, _ = prepareResponseWithErr(httpStatusCode, nil)
	response.Status = okMsg
	return response, nil
}

func prepareResponseWithErr(httpStatusCode int, e error) (response snapshotReportResponse, err error) {
	response.Version = APIVer
	response.ResponseCode = httpStatusCode
	if e != nil {
		response.Status = e.Error()
	}
	return response, e
}

// cancel a running job
func (j *SnapshotJob) cancel(config *Config, DCOSTools DCOSHelper) (response snapshotReportResponse, err error) {
	role, err := DCOSTools.GetNodeRole()
	if err != nil {
		// Just log the error. We can still try to cancel the job.
		log.Error(err)
	}
	if role == "agent" {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("Canceling snapshot job on agent node is not implemented."))
	}

	// return error if we could not find if the job is running or not.
	isRunning, node, err := j.isRunning(config, DCOSTools)
	if err != nil {
		return response, err
	}

	if !isRunning {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("Job is not running"))
	}
	// if node is empty, try to cancel a job on a localhost
	if node == "" {
		j.cancelChan <- true
		log.Debug("Cancelling a local job")
	} else {
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/cancel", node, config.FlagPort, BaseRoute)
		j.Status = "Attempting to cancel a job on a remote host. POST " + url
		log.Debug(j.Status)
		response, _, err := DCOSTools.Post(url, time.Duration(time.Second*3))
		if err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		// unmarshal a response from a remote node and return it back.
		var remoteResponse snapshotReportResponse
		if err = json.Unmarshal(response, &remoteResponse); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		return remoteResponse, nil

	}
	return prepareResponseOk(http.StatusOK, "Attempting to cancel a job, please check job status.")
}

func (j *SnapshotJob) start() {
	j.Running = true
}

func (j *SnapshotJob) stop() {
	j.Running = false
}

// get a list of all snapshots across the cluster.
func listAllSnapshots(config *Config, DCOSTools DCOSHelper) (map[string][]string, error) {
	collectedSnapshots := make(map[string][]string)
	masterNodes, err := DCOSTools.GetMasterNodes()
	if err != nil {
		return collectedSnapshots, err
	}
	for _, master := range masterNodes {
		var snapshotUrls []string
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/list", master.IP, config.FlagPort, BaseRoute)
		body, _, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err != nil {
			log.Error(err)
			continue
		}
		if err = json.Unmarshal(body, &snapshotUrls); err != nil {
			log.Error(err)
			continue
		}
		collectedSnapshots[fmt.Sprintf("%s:%d", master.IP, config.FlagPort)] = snapshotUrls
	}
	return collectedSnapshots, nil
}

// check if the snapshot is available on a cluster.
func (j *SnapshotJob) isSnapshotAvailable(snapshotName string, config *Config, DCOSTools DCOSHelper) (string, string, bool, error) {
	snapshots, err := listAllSnapshots(config, DCOSTools)
	if err != nil {
		return "", "", false, err
	}
	log.Infof("Trying to find a snapshot %s on remote hosts", snapshotName)
	for host, remoteSnapshots := range snapshots {
		for _, remoteSnapshot := range remoteSnapshots {
			if snapshotName == path.Base(remoteSnapshot) {
				log.Infof("Snapshot %s found on a host: %s", snapshotName, host)
				hostPort := strings.Split(host, ":")
				if len(hostPort) > 0 {
					return hostPort[0], remoteSnapshot, true, nil
				}
				return "", "", false, errors.New("Node must be ip:port. Got " + host)
			}
		}
	}
	return "", "", false, nil
}

// return a a list of snapshots available on a localhost.
func (j *SnapshotJob) findLocalSnapshot(config *Config) (snapshots []string, err error) {
	matches, err := filepath.Glob(config.FlagSnapshotDir + "/snapshot-*.zip")
	for _, snapshot := range matches {
		// skip a snapshot zip file if the job is running
		if snapshot == j.LastSnapshotPath && j.Running {
			log.Infof("Skipped listing %s, the job is running", snapshot)
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	if err != nil {
		return snapshots, err
	}
	return snapshots, nil
}

func findRequestedNodes(masterNodes []Node, agentNodes []Node, requestedNodes []string) (matchedNodes []Node, err error) {
	clusterNodes := append(masterNodes, agentNodes...)
	for _, requestedNode := range requestedNodes {
		if requestedNode == "all" {
			return clusterNodes, nil
		}
		if requestedNode == "masters" {
			matchedNodes = append(matchedNodes, masterNodes...)
		}
		if requestedNode == "agents" {
			matchedNodes = append(matchedNodes, agentNodes...)
		}
		// try to find nodes by ip / mesos id
		for _, clusterNode := range clusterNodes {
			if requestedNode == clusterNode.IP || requestedNode == clusterNode.MesosID || requestedNode == clusterNode.Host {
				matchedNodes = append(matchedNodes, clusterNode)
			}
		}
	}
	if len(matchedNodes) > 0 {
		return matchedNodes, nil
	}
	return matchedNodes, fmt.Errorf("Requested nodes: %s not found", requestedNodes)
}

// LogProviders a structure defines a list of Providers
type LogProviders struct {
	HTTPEndpoints []HTTPProvider
	LocalFiles    []FileProvider
	LocalCommands []CommandProvider
}

// HTTPProvider is a provider for fetching an HTTP endpoint.
type HTTPProvider struct {
	Port     int
	URI      string
	FileName string
	Role     string
}

// FileProvider is a local file provider.
type FileProvider struct {
	Location string
	Role     string
}

// CommandProvider is a local command to execute.
type CommandProvider struct {
	Command []string
	Role    string
}

func loadExternalProviders(config *Config) (LogProviders, error) {
	var externalProviders LogProviders
	endpointsConfig, err := ioutil.ReadFile(config.FlagSnapshotEndpointsConfigFile)
	if err != nil {
		return externalProviders, err
	}
	if err = json.Unmarshal(endpointsConfig, &externalProviders); err != nil {
		return externalProviders, err
	}
	return externalProviders, nil
}

func loadInternalProviders(config *Config, DCOSTools DCOSHelper) (internalConfigProviders LogProviders, err error) {
	units, err := DCOSTools.GetUnitNames()
	if err != nil {
		return
	}

	// load default HTTP
	var httpEndpoints []HTTPProvider
	for _, unit := range append(units, config.SystemdUnits...) {
		httpEndpoints = append(httpEndpoints, HTTPProvider{
			Port:     config.FlagPort,
			URI:      fmt.Sprintf("%s/logs/units/%s", BaseRoute, unit),
			FileName: unit + ".log",
		})
	}
	httpEndpoints = append(httpEndpoints, HTTPProvider{
		Port:     1050,
		URI:      BaseRoute,
		FileName: "3dt-health.log",
	})

	return LogProviders{
		HTTPEndpoints: httpEndpoints,
	}, nil
}

// get a list of all available endpoints
func getLogsEndpointList(config *Config, DCOSTools DCOSHelper) (endpoints map[string]string, err error) {
	internalProviders, err := loadInternalProviders(config, DCOSTools)
	if err != nil {
		log.Error(err)
	}
	externalProviders, err := loadExternalProviders(config)
	if err != nil {
		log.Error(err)
	}

	endpoints = make(map[string]string)

	role, err := DCOSTools.GetNodeRole()
	if err != nil {
		return endpoints, err
	}

	// Load HTTP endpoints
	for _, endpoint := range append(internalProviders.HTTPEndpoints, externalProviders.HTTPEndpoints...) {
		// if role is set and does not equal current role, skip.
		if endpoint.Role != "" && endpoint.Role != role {
			continue
		}
		endpoints[endpoint.FileName] = fmt.Sprintf(":%d%s", endpoint.Port, endpoint.URI)
	}

	// Load file endpoints
	for _, file := range append(internalProviders.LocalFiles, externalProviders.LocalFiles...) {
		// if role is set and does not equal current role, skip.
		if file.Role != "" && file.Role != role {
			continue
		}
		endpoints[file.Location] = fmt.Sprintf(":%d%s/logs/files/%s", config.FlagPort, BaseRoute, filepath.Base(file.Location))
	}

	// Load command endpoints
	for _, c := range append(internalProviders.LocalCommands, externalProviders.LocalCommands...) {
		// if role is set and does not equal current role, skip.
		if c.Role != "" && c.Role != role {
			continue
		}
		if len(c.Command) > 0 {
			endpoints[filepath.Base(c.Command[0])+".output"] = fmt.Sprintf(":%d%s/logs/cmds/%s", config.FlagPort, BaseRoute, filepath.Base(c.Command[0]))
		}
	}
	return endpoints, nil
}

// the summary report is a file added to a zip snapshot file to track any errors occured while collection logs.
func updateSummaryReport(preflix string, node Node, error string, r *bytes.Buffer) {
	r.WriteString(fmt.Sprintf("%s [%s] %s %s %s\n", time.Now().String(), preflix, node.IP, node.Role, error))
}
