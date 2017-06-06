package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/config"
	"github.com/dcos/dcos-go/exec"
	"github.com/shirou/gopsutil/disk"
)

const (
	// All stands for collecting logs from all discovered nodes.
	All = "all"

	// Masters stand for collecting from discovered master nodes.
	Masters = "masters"

	// Agents stand for collecting from discovered agent/agent_public nodes.
	Agents = "agents"
)

// DiagnosticsJob is the main structure for a logs collection job.
type DiagnosticsJob struct {
	sync.Mutex
	cancelChan   chan bool
	logProviders *LogProviders

	Transport             http.RoundTripper `json:"-"`
	Running               bool              `json:"is_running"`
	Status                string            `json:"status"`
	Errors                []string          `json:"errors"`
	LastBundlePath        string            `json:"last_bundle_dir"`
	JobStarted            time.Time         `json:"job_started"`
	JobEnded              time.Time         `json:"job_ended"`
	JobDuration           time.Duration     `json:"job_duration"`
	JobProgressPercentage float32           `json:"job_progress_percentage"`
}

// diagnostics job response format
type diagnosticsReportResponse struct {
	ResponseCode int      `json:"response_http_code"`
	Version      int      `json:"version"`
	Status       string   `json:"status"`
	Errors       []string `json:"errors"`
}

type createResponse struct {
	diagnosticsReportResponse
	Extra struct {
		LastBundleFile string `json:"bundle_name"`
	} `json:"extra"`
}

// diagnostics job status format
type bundleReportStatus struct {
	// job related fields
	Running               bool     `json:"is_running"`
	Status                string   `json:"status"`
	Errors                []string `json:"errors"`
	LastBundlePath        string   `json:"last_bundle_dir"`
	JobStarted            string   `json:"job_started"`
	JobEnded              string   `json:"job_ended"`
	JobDuration           string   `json:"job_duration"`
	JobProgressPercentage float32  `json:"job_progress_percentage"`

	// config related fields
	DiagnosticBundlesBaseDir                 string `json:"diagnostics_bundle_dir"`
	DiagnosticsJobTimeoutMin                 int    `json:"diagnostics_job_timeout_min"`
	DiagnosticsUnitsLogsSinceHours           string `json:"journald_logs_since_hours"`
	DiagnosticsJobGetSingleURLTimeoutMinutes int    `json:"diagnostics_job_get_since_url_timeout_min"`
	CommandExecTimeoutSec                    int    `json:"command_exec_timeout_sec"`

	// metrics related
	DiskUsedPercent float64 `json:"diagnostics_partition_disk_usage_percent"`
}

// Create a bundle request structure, example:   {"nodes": ["all"]}
type bundleCreateRequest struct {
	Version int
	Nodes   []string
}

// start a diagnostics job
func (j *DiagnosticsJob) run(req bundleCreateRequest, dt *Dt) (createResponse, error) {

	role, err := dt.DtDCOSTools.GetNodeRole()
	if err != nil {
		return prepareCreateResponseWithErr(http.StatusServiceUnavailable, err)
	}

	if role == AgentRole || role == AgentPublicRole {
		return prepareCreateResponseWithErr(http.StatusServiceUnavailable, errors.New("running diagnostics job on agent node is not implemented"))
	}

	isRunning, _, err := j.isRunning(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		return prepareCreateResponseWithErr(http.StatusServiceUnavailable, err)
	}
	if isRunning {
		return prepareCreateResponseWithErr(http.StatusServiceUnavailable, errors.New("Job is already running"))
	}

	foundNodes, err := findRequestedNodes(req.Nodes, dt)
	if err != nil {
		return prepareCreateResponseWithErr(http.StatusServiceUnavailable, err)
	}
	logrus.Debugf("Found requested nodes: %s", foundNodes)

	// try to create directory for diagnostic bundles
	_, err = os.Stat(dt.Cfg.FlagDiagnosticsBundleDir)
	if os.IsNotExist(err) {
		logrus.Infof("Directory: %s not found, attempting to create one", dt.Cfg.FlagDiagnosticsBundleDir)
		if err := os.Mkdir(dt.Cfg.FlagDiagnosticsBundleDir, os.ModePerm); err != nil {
			j.Status = "Could not create directory: " + dt.Cfg.FlagDiagnosticsBundleDir
			return prepareCreateResponseWithErr(http.StatusServiceUnavailable, errors.New(j.Status))
		}
	}

	// Null errors on every new run.
	j.Errors = nil

	t := time.Now()
	bundleName := fmt.Sprintf("bundle-%d-%02d-%02dT%02d:%02d:%02d-%d.zip", t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(), t.Nanosecond())

	j.LastBundlePath = filepath.Join(dt.Cfg.FlagDiagnosticsBundleDir, bundleName)
	j.Status = "Diagnostics job started, archive will be available at: " + j.LastBundlePath

	j.cancelChan = make(chan bool)
	go j.runBackgroundJob(foundNodes, dt.Cfg, dt.DtDCOSTools)

	var r createResponse
	r.Extra.LastBundleFile = bundleName
	r.ResponseCode = http.StatusOK
	r.Version = config.APIVer
	r.Status = "Job has been successfully started"
	return r, nil
}

//
func (j *DiagnosticsJob) runBackgroundJob(nodes []Node, cfg *config.Config, DCOSTools DCOSHelper) {
	if len(nodes) == 0 {
		e := "Nodes length cannot be 0"
		j.Status = "Job failed"
		j.Errors = append(j.Errors, e)
		return
	}
	logrus.Info("Started background job")

	// log a start time
	j.JobStarted = time.Now()

	// log end time
	defer func(j *DiagnosticsJob) {
		j.JobEnded = time.Now()
		j.JobDuration = time.Since(j.JobStarted)
		logrus.Info("Job finished")
	}(j)

	// lets start a goroutine which will timeout background report job after a certain time.
	jobIsDone := make(chan bool)
	go func(jobIsDone chan bool, j *DiagnosticsJob) {
		select {
		case <-jobIsDone:
			return
		case <-time.After(time.Minute * time.Duration(cfg.FlagDiagnosticsJobTimeoutMinutes)):
			errMsg := fmt.Sprintf("diagnostics job timedout after: %s", time.Since(j.JobStarted))
			j.Lock()
			j.Status = "Job failed"
			j.Errors = append(j.Errors, errMsg)
			j.Unlock()
			logrus.Error(errMsg)
			j.cancelChan <- true
			return
		}
	}(jobIsDone, j)

	// make sure we always cancel a timeout goroutine when the report is finished.
	defer func(jobIsDone chan bool) {
		jobIsDone <- true
	}(jobIsDone)

	// Update job running field.
	j.start()
	defer j.stop()

	// create a zip file
	zipfile, err := os.Create(j.LastBundlePath)
	if err != nil {
		j.Status = "Job failed"
		errMsg := fmt.Sprintf("Could not create zip file: %s", j.LastBundlePath)
		j.Errors = append(j.Errors, errMsg)
		logrus.Error(errMsg)
		return
	}
	defer zipfile.Close()

	zipWriter := zip.NewWriter(zipfile)
	defer zipWriter.Close()

	// summaryReport is a log of a diagnostics job
	summaryReport := new(bytes.Buffer)

	// place a summaryErrorsReport.txt in a zip archive which should provide info what failed during the logs collection.
	summaryErrorsReport := new(bytes.Buffer)
	defer func() {
		zipFile, err := zipWriter.Create("summaryErrorsReport.txt")
		if err != nil {
			j.Status = "Could not append a summaryErrorsReport.txt to a zip file"
			logrus.Errorf("%s: %s", j.Status, err)
			j.Errors = append(j.Errors, err.Error())
			return
		}
		io.Copy(zipFile, summaryErrorsReport)

		// flush the summary report
		zipFile, err = zipWriter.Create("summaryReport.txt")
		if err != nil {
			j.Status = "Could not append a summaryReport.txt to a zip file"
			logrus.Errorf("%s: %s", j.Status, err)
			j.Errors = append(j.Errors, err.Error())
			return
		}
		io.Copy(zipFile, summaryReport)
	}()

	// lock out reportJob staructure
	j.Lock()
	defer j.Unlock()

	// reset counters
	j.JobDuration = 0
	j.JobProgressPercentage = 0

	// we already checked for nodes length, we should not get division by zero error at this point.
	percentPerNode := 100.0 / float32(len(nodes))
	for _, node := range nodes {
		port, err := getPullPortByRole(cfg, node.Role)
		if err != nil {
			logrus.Errorf("Used incorrect role: %s", err)
			j.Errors = append(j.Errors, err.Error())
			updateSummaryReport("Used incorrect role", node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerNode
			continue
		}

		updateSummaryReport("START collecting logs", node, "", summaryReport)
		url := fmt.Sprintf("http://%s:%d%s/logs", node.IP, port, BaseRoute)
		endpoints := make(map[string]string)
		body, statusCode, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err != nil {
			errMsg := fmt.Sprintf("could not get a list of logs, url: %s, status code %d", url, statusCode)
			j.Errors = append(j.Errors, errMsg)
			logrus.Errorf("%s: %s", errMsg, err)
			updateSummaryReport(errMsg, node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerNode
			continue
		}
		if err = json.Unmarshal(body, &endpoints); err != nil {
			errMsg := "could not unmarshal a list of logs, url: " + url
			j.Errors = append(j.Errors, errMsg)
			logrus.Errorf("%s: %s", errMsg, err)
			updateSummaryReport(errMsg, node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerNode
			continue
		}

		if len(endpoints) == 0 {
			errMsg := "No endpoints found, url: " + url
			j.Errors = append(j.Errors, errMsg)
			logrus.Error(errMsg)
			updateSummaryReport(errMsg, node, "", summaryErrorsReport)
			j.JobProgressPercentage += percentPerNode
			continue
		}
		// add http endpoints
		err = j.getHTTPAddToZip(node, endpoints, j.LastBundlePath, zipWriter, summaryErrorsReport,
			summaryReport, cfg, DCOSTools, percentPerNode)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())

			// handle job cancel error
			if serr, ok := err.(diagnosticsJobCanceledError); ok {
				logrus.Errorf("Could not add diagnostics to zip file: %s", serr)
				j.LastBundlePath = ""
				if removeErr := os.Remove(zipfile.Name()); removeErr != nil {
					logrus.Errorf("Could not remove a bundle: %s", removeErr)
					j.Errors = append(j.Errors, removeErr.Error())
				}
				return
			}

			logrus.Errorf("Could not add a log to a bundle: %s", err)
			updateSummaryReport(err.Error(), node, err.Error(), summaryErrorsReport)
		}
		updateSummaryReport("STOP collecting logs", node, "", summaryReport)
	}
	j.JobProgressPercentage = 100
	if len(j.Errors) == 0 {
		j.Status = "Diagnostics job sucessfully finished"
	} else {
		j.Status = "Diagnostics job failed"
	}
}

// delete a bundle
func (j *DiagnosticsJob) delete(bundleName string, cfg *config.Config, DCOSTools DCOSHelper) (response diagnosticsReportResponse, err error) {
	if !strings.HasPrefix(bundleName, "bundle-") || !strings.HasSuffix(bundleName, ".zip") {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("format allowed  bundle-*.zip"))
	}

	j.Lock()
	defer j.Unlock()

	// first try to locate a bundle on a local disk.
	bundlePath := path.Join(cfg.FlagDiagnosticsBundleDir, bundleName)
	logrus.Debugf("Trying remove a bundle: %s", bundlePath)
	_, err = os.Stat(bundlePath)
	if err == nil {
		if err = os.Remove(bundlePath); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		msg := "Deleted " + bundlePath
		logrus.Infof(msg)
		return prepareResponseOk(http.StatusOK, msg)
	}

	node, _, ok, err := j.isBundleAvailable(bundleName, cfg, DCOSTools)
	if err != nil {
		return prepareResponseWithErr(http.StatusServiceUnavailable, err)
	}
	if ok {
		url := fmt.Sprintf("http://%s:%d%s/report/diagnostics/delete/%s", node, cfg.FlagMasterPort, BaseRoute, bundleName)
		j.Status = "Attempting to delete a bundle on a remote host. POST " + url
		logrus.Debug(j.Status)
		timeout := time.Duration(time.Second * 5)
		response, _, err := DCOSTools.Post(url, timeout)
		if err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		// unmarshal a response from a remote node and return it back.
		var remoteResponse diagnosticsReportResponse
		if err = json.Unmarshal(response, &remoteResponse); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		j.Status = remoteResponse.Status
		return remoteResponse, nil
	}
	j.Status = "Bundle not found " + bundleName
	return prepareResponseOk(http.StatusNotFound, j.Status)
}

// isRunning returns if the diagnostics job is running, node the job is running on and error. If the node is empty
// string, then the job is running on a localhost.
func (j *DiagnosticsJob) isRunning(cfg *config.Config, DCOSTools DCOSHelper) (bool, string, error) {
	// first check if the job is running on a localhost.
	if j.Running {
		return true, "", nil
	}

	// try to discover if the job is running on other masters.
	clusterDiagnosticsJobStatus, err := j.getStatusAll(cfg, DCOSTools)
	if err != nil {
		return false, "", err
	}
	for node, status := range clusterDiagnosticsJobStatus {
		if status.Running == true {
			return true, node, nil
		}
	}

	// no running job found.
	return false, "", nil
}

// Collect all status reports from master nodes and return a map[master_ip] bundleReportStatus
// The function is used to get a job status on other nodes
func (j *DiagnosticsJob) getStatusAll(cfg *config.Config, DCOSTools DCOSHelper) (map[string]bundleReportStatus, error) {
	statuses := make(map[string]bundleReportStatus)

	masterNodes, err := DCOSTools.GetMasterNodes()
	if err != nil {
		return statuses, err
	}

	for _, master := range masterNodes {
		var status bundleReportStatus
		url := fmt.Sprintf("http://%s:%d%s/report/diagnostics/status", master.IP, cfg.FlagMasterPort, BaseRoute)
		body, _, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err = json.Unmarshal(body, &status); err != nil {
			logrus.Errorf("Could not determine job status for node %s: %s", master.IP, err)
			continue
		}
		statuses[master.IP] = status
	}
	if len(statuses) == 0 {
		return statuses, errors.New("could not determine wheather the diagnostics job is running or not")
	}
	return statuses, nil
}

// get a status report for a localhost
func (j *DiagnosticsJob) getStatus(cfg *config.Config) bundleReportStatus {
	// use a temp var `used`, since disk.Usage panics if partition does not exist.
	var used float64
	usageStat, err := disk.Usage(cfg.FlagDiagnosticsBundleDir)
	if err == nil {
		used = usageStat.UsedPercent
	} else {
		logrus.Errorf("Could not get a disk usage %s: %s", cfg.FlagDiagnosticsBundleDir, err)
	}
	return bundleReportStatus{
		Running:               j.Running,
		Status:                j.Status,
		Errors:                j.Errors,
		LastBundlePath:        j.LastBundlePath,
		JobStarted:            j.JobStarted.String(),
		JobEnded:              j.JobEnded.String(),
		JobDuration:           j.JobDuration.String(),
		JobProgressPercentage: j.JobProgressPercentage,

		DiagnosticBundlesBaseDir:                 cfg.FlagDiagnosticsBundleDir,
		DiagnosticsJobTimeoutMin:                 cfg.FlagDiagnosticsJobTimeoutMinutes,
		DiagnosticsJobGetSingleURLTimeoutMinutes: cfg.FlagDiagnosticsJobGetSingleURLTimeoutMinutes,
		DiagnosticsUnitsLogsSinceHours:           cfg.FlagDiagnosticsBundleUnitsLogsSinceString,
		CommandExecTimeoutSec:                    cfg.FlagCommandExecTimeoutSec,

		DiskUsedPercent: used,
	}
}

type diagnosticsJobCanceledError struct {
	msg string
}

func (d diagnosticsJobCanceledError) Error() string {
	return d.msg
}

// fetch an HTTP endpoint and append the output to a zip file.
func (j *DiagnosticsJob) getHTTPAddToZip(node Node, endpoints map[string]string, folder string, zipWriter *zip.Writer,
	summaryErrorsReport, summaryReport *bytes.Buffer, cfg *config.Config, DCOSTools DCOSHelper, percentPerNode float32) error {
	if len(endpoints) == 0 || percentPerNode == 0 {
		j.JobProgressPercentage += percentPerNode
		return fmt.Errorf("`endpoints` length or `percentPerNode` arguments cannot be empty. Got: %s, %f", endpoints, percentPerNode)
	}

	percentPerURL := percentPerNode / float32(len(endpoints))
	for fileName, httpEndpoint := range endpoints {
		fullURL, err := useTLSScheme("http://"+node.IP+httpEndpoint, cfg.FlagForceTLS)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			logrus.Errorf("Could not read force-tls flag: %s", err)
			updateSummaryReport(err.Error(), node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerURL
			continue
		}
		logrus.Debugf("Using URL %s to collect a log", fullURL)
		select {
		case _, ok := <-j.cancelChan:
			if ok {
				updateSummaryReport("Job canceled", node, "", summaryErrorsReport)
				updateSummaryReport("Job canceled", node, "", summaryReport)
				return diagnosticsJobCanceledError{
					msg: "Job canceled",
				}
			}

		default:
			logrus.Debugf("GET %s", fullURL)
		}

		j.Status = "GET " + fullURL
		updateSummaryReport("START "+j.Status, node, "", summaryReport)
		timeout := time.Duration(time.Minute * time.Duration(cfg.FlagDiagnosticsJobGetSingleURLTimeoutMinutes))
		request, err := http.NewRequest("GET", fullURL, nil)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			logrus.Errorf("Could not create a new HTTP request: %s", err)
			updateSummaryReport(fmt.Sprintf("could not create request for url: %s", fullURL), node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerURL
			continue
		}
		request.Header.Add("Accept-Encoding", "gzip")

		client := NewHTTPClient(timeout, j.Transport)
		resp, err := client.Do(request)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			logrus.Errorf("Could not fetch url %s: %s", fullURL, err)
			updateSummaryReport(fmt.Sprintf("could not fetch url: %s", fullURL), node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerURL
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
			logrus.Errorf("Could not add %s to a zip archive: %s", fileName, err)
			updateSummaryReport(fmt.Sprintf("could not add a file %s to a zip", fileName), node, err.Error(), summaryErrorsReport)
			j.JobProgressPercentage += percentPerURL
			continue
		}
		io.Copy(zipFile, resp.Body)
		resp.Body.Close()
		updateSummaryReport("STOP "+j.Status, node, "", summaryReport)
		j.JobProgressPercentage += percentPerURL
	}
	return nil
}

func prepareResponseOk(httpStatusCode int, okMsg string) (response diagnosticsReportResponse, err error) {
	response, _ = prepareResponseWithErr(httpStatusCode, nil)
	response.Status = okMsg
	return response, nil
}

func prepareResponseWithErr(httpStatusCode int, e error) (response diagnosticsReportResponse, err error) {
	response.Version = config.APIVer
	response.ResponseCode = httpStatusCode
	if e != nil {
		response.Status = e.Error()
	}
	return response, e
}

func prepareCreateResponseWithErr(httpStatusCode int, e error) (createResponse, error) {
	cr := createResponse{}
	cr.ResponseCode = httpStatusCode
	cr.Version = config.APIVer
	if e != nil {
		cr.Status = e.Error()
	}
	return cr, e
}

// cancel a running job
func (j *DiagnosticsJob) cancel(cfg *config.Config, DCOSTools DCOSHelper) (response diagnosticsReportResponse, err error) {
	role, err := DCOSTools.GetNodeRole()
	if err != nil {
		// Just log the error. We can still try to cancel the job.
		logrus.Errorf("Could not detect node role: %s", err)
	}
	if role == AgentRole || role == AgentPublicRole {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("canceling diagnostics job on agent node is not implemented"))
	}

	// return error if we could not find if the job is running or not.
	isRunning, node, err := j.isRunning(cfg, DCOSTools)
	if err != nil {
		return response, err
	}

	if !isRunning {
		return prepareResponseWithErr(http.StatusServiceUnavailable, errors.New("Job is not running"))
	}
	// if node is empty, try to cancel a job on a localhost
	if node == "" {
		j.cancelChan <- true
		logrus.Debug("Cancelling a local job")
	} else {
		url := fmt.Sprintf("http://%s:%d%s/report/diagnostics/cancel", node, cfg.FlagMasterPort, BaseRoute)
		j.Status = "Attempting to cancel a job on a remote host. POST " + url
		logrus.Debug(j.Status)
		response, _, err := DCOSTools.Post(url, time.Duration(cfg.FlagDiagnosticsJobGetSingleURLTimeoutMinutes)*time.Minute)
		if err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		// unmarshal a response from a remote node and return it back.
		var remoteResponse diagnosticsReportResponse
		if err = json.Unmarshal(response, &remoteResponse); err != nil {
			return prepareResponseWithErr(http.StatusServiceUnavailable, err)
		}
		return remoteResponse, nil

	}
	return prepareResponseOk(http.StatusOK, "Attempting to cancel a job, please check job status.")
}

func (j *DiagnosticsJob) start() {
	j.Running = true
}

func (j *DiagnosticsJob) stop() {
	j.Running = false
}

// get a list of all bundles across the cluster.
func listAllBundles(cfg *config.Config, DCOSTools DCOSHelper) (map[string][]bundle, error) {
	collectedBundles := make(map[string][]bundle)
	masterNodes, err := DCOSTools.GetMasterNodes()
	if err != nil {
		return collectedBundles, err
	}
	for _, master := range masterNodes {
		var bundleUrls []bundle
		url := fmt.Sprintf("http://%s:%d%s/report/diagnostics/list", master.IP, cfg.FlagMasterPort, BaseRoute)
		body, _, err := DCOSTools.Get(url, time.Duration(time.Second*3))
		if err != nil {
			logrus.Errorf("Could not HTTP GET %s: %s", url, err)
			continue
		}
		if err = json.Unmarshal(body, &bundleUrls); err != nil {
			logrus.Errorf("Could not unmarshal response from %s: %s", url, err)
			continue
		}
		collectedBundles[fmt.Sprintf("%s:%d", master.IP, cfg.FlagMasterPort)] = bundleUrls
	}
	return collectedBundles, nil
}

// check if a bundle is available on a cluster.
func (j *DiagnosticsJob) isBundleAvailable(bundleName string, cfg *config.Config, DCOSTools DCOSHelper) (string, string, bool, error) {
	bundles, err := listAllBundles(cfg, DCOSTools)
	if err != nil {
		return "", "", false, err
	}
	logrus.Infof("Trying to find a bundle %s on remote hosts", bundleName)
	for host, remoteBundles := range bundles {
		for _, remoteBundle := range remoteBundles {
			if bundleName == path.Base(remoteBundle.File) {
				logrus.Infof("Bundle %s found on a host: %s", bundleName, host)
				hostPort := strings.Split(host, ":")
				if len(hostPort) > 0 {
					return hostPort[0], remoteBundle.File, true, nil
				}
				return "", "", false, errors.New("Node must be ip:port. Got " + host)
			}
		}
	}
	return "", "", false, nil
}

// return a a list of bundles available on a localhost.
func (j *DiagnosticsJob) findLocalBundle(cfg *config.Config) (bundles []string, err error) {
	matches, err := filepath.Glob(cfg.FlagDiagnosticsBundleDir + "/bundle-*.zip")
	for _, localBundle := range matches {
		// skip a bundle zip file if the job is running
		if localBundle == j.LastBundlePath && j.Running {
			logrus.Infof("Skipped listing %s, the job is running", localBundle)
			continue
		}
		bundles = append(bundles, localBundle)
	}
	if err != nil {
		return bundles, err
	}
	return bundles, nil
}

func matchRequestedNodes(requestedNodes []string, masterNodes []Node, agentNodes []Node) ([]Node, error) {
	var matchedNodes []Node
	clusterNodes := append(masterNodes, agentNodes...)
	if len(requestedNodes) == 0 || len(clusterNodes) == 0 {
		return matchedNodes, errors.New("Cannot match requested nodes to clusterNodes")
	}

	for _, requestedNode := range requestedNodes {
		if requestedNode == "" {
			continue
		}

		if requestedNode == All {
			return clusterNodes, nil
		}
		if requestedNode == Masters {
			matchedNodes = append(matchedNodes, masterNodes...)
		}
		if requestedNode == Agents {
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

func findRequestedNodes(requestedNodes []string, dt *Dt) ([]Node, error) {
	var masterNodes, agentNodes []Node
	masterNodes, agentNodes, err := dt.MR.GetMasterAgentNodes()
	if err != nil {
		// failed to find master and agent nodes in memory. Try to discover
		logrus.Errorf("Could not find masters or agents in memory: %s", err)
		masterNodes, err = dt.DtDCOSTools.GetMasterNodes()
		if err != nil {
			logrus.Errorf("Could not get master nodes: %s", err)
		}

		agentNodes, err = dt.DtDCOSTools.GetAgentNodes()
		if err != nil {
			logrus.Errorf("Could not get agent nodes: %s", err)
		}
	}
	return matchRequestedNodes(requestedNodes, masterNodes, agentNodes)
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
	Role     []string
}

// FileProvider is a local file provider.
type FileProvider struct {
	Location          string
	Role              []string
	sanitizedLocation string
}

// CommandProvider is a local command to execute.
type CommandProvider struct {
	Command        []string
	Role           []string
	indexedCommand string
}

func loadExternalProviders(cfg *config.Config) (externalProviders LogProviders, err error) {
	// return if cfg file not found.
	if _, err = os.Stat(cfg.FlagDiagnosticsBundleEndpointsConfigFile); err != nil {
		if os.IsNotExist(err) {
			logrus.Infof("%s not found", cfg.FlagDiagnosticsBundleEndpointsConfigFile)
			return externalProviders, nil
		}
	}

	endpointsConfig, err := ioutil.ReadFile(cfg.FlagDiagnosticsBundleEndpointsConfigFile)
	if err != nil {
		return externalProviders, err
	}
	if err = json.Unmarshal(endpointsConfig, &externalProviders); err != nil {
		return externalProviders, err
	}
	return externalProviders, nil
}

func loadInternalProviders(cfg *config.Config, DCOSTools DCOSHelper) (internalConfigProviders LogProviders, err error) {
	units, err := DCOSTools.GetUnitNames()
	if err != nil {
		return internalConfigProviders, err
	}

	role, err := DCOSTools.GetNodeRole()
	if err != nil {
		return internalConfigProviders, err
	}

	port, err := getPullPortByRole(cfg, role)
	if err != nil {
		return internalConfigProviders, err
	}

	// load default HTTP
	var httpEndpoints []HTTPProvider
	for _, unit := range append(units, cfg.SystemdUnits...) {
		httpEndpoints = append(httpEndpoints, HTTPProvider{
			Port:     port,
			URI:      fmt.Sprintf("%s/logs/units/%s", BaseRoute, unit),
			FileName: unit,
		})
	}

	// add 3dt health report.
	httpEndpoints = append(httpEndpoints, HTTPProvider{
		Port:     port,
		URI:      BaseRoute,
		FileName: "3dt-health.json",
	})

	return LogProviders{
		HTTPEndpoints: httpEndpoints,
	}, nil
}

func (j *DiagnosticsJob) getLogsEndpoints(cfg *config.Config, DCOSTools DCOSHelper) (endpoints map[string]string, err error) {
	endpoints = make(map[string]string)
	if j.logProviders == nil {
		return endpoints, errors.New("log provders have not been initialized")
	}

	currentRole, err := DCOSTools.GetNodeRole()
	if err != nil {
		logrus.Errorf("Failed to get a current role for a cfg: %s", err)
	}

	port, err := getPullPortByRole(cfg, currentRole)
	if err != nil {
		return endpoints, err
	}

	matchRole := func(currentRole string, roles []string) bool {
		// if roles is empty, use for all roles.
		if len(roles) == 0 {
			return true
		}

		for _, role := range roles {
			if role == currentRole {
				return true
			}
		}
		return false
	}

	// http endpoints
	for _, httpEndpoint := range j.logProviders.HTTPEndpoints {
		// if a role wasn't detected, consider to load all endpoints from a cfg file.
		// if the role could not be detected or it is not set in a cfg file use the log endpoint.
		// do not use the role only if it is set, detected and does not match the role form a cfg.
		if !matchRole(currentRole, httpEndpoint.Role) {
			continue
		}
		endpoints[httpEndpoint.FileName] = fmt.Sprintf(":%d%s", httpEndpoint.Port, httpEndpoint.URI)
	}

	// file endpoints
	for _, file := range j.logProviders.LocalFiles {
		if !matchRole(currentRole, file.Role) {
			continue
		}
		endpoints[file.Location] = fmt.Sprintf(":%d%s/logs/files/%s", port, BaseRoute, file.sanitizedLocation)
	}

	// command endpoints
	for _, c := range j.logProviders.LocalCommands {
		if !matchRole(currentRole, c.Role) {
			continue
		}
		if c.indexedCommand != "" {
			endpoints[c.indexedCommand] = fmt.Sprintf(":%d%s/logs/cmds/%s", port, BaseRoute, c.indexedCommand)
		}
	}
	return endpoints, nil
}

// Init will prepare diagnostics job, read config files etc.
func (j *DiagnosticsJob) Init(cfg *config.Config, DCOSTools DCOSHelper) error {
	j.logProviders = &LogProviders{}

	// set JobProgressPercentage -1 means the job has never been executed
	j.JobProgressPercentage = -1

	// load the internal providers
	internalProviders, err := loadInternalProviders(cfg, DCOSTools)
	if err != nil {
		logrus.Errorf("Could not initialize internal log provders: %s", err)
	}

	// load the external providers from a cfg file
	externalProviders, err := loadExternalProviders(cfg)
	if err != nil {
		logrus.Errorf("Could not initialize external log provders: %s", err)
	}

	j.logProviders.HTTPEndpoints = append(internalProviders.HTTPEndpoints, externalProviders.HTTPEndpoints...)
	j.logProviders.LocalFiles = append(internalProviders.LocalFiles, externalProviders.LocalFiles...)
	j.logProviders.LocalCommands = append(internalProviders.LocalCommands, externalProviders.LocalCommands...)

	// set filename if not set
	for index, endpoint := range j.logProviders.HTTPEndpoints {
		if endpoint.FileName == "" {
			sanitizedPath := strings.Replace(strings.TrimLeft(endpoint.URI, "/"), "/", "_", -1)
			j.logProviders.HTTPEndpoints[index].FileName = fmt.Sprintf("%d:%s.json", endpoint.Port, sanitizedPath)
		}
	}

	// trim left "/" and replace all slashes with underscores.
	for index, fileProvider := range j.logProviders.LocalFiles {
		j.logProviders.LocalFiles[index].sanitizedLocation = strings.Replace(strings.TrimLeft(fileProvider.Location, "/"), "/", "_", -1)
	}

	// update command with index.
	for index, commandProvider := range j.logProviders.LocalCommands {
		if len(commandProvider.Command) > 0 {
			cmdWithArgs := strings.Join(commandProvider.Command, "_")
			trimmedCmdWithArgs := strings.Replace(cmdWithArgs, "/", "", -1)
			j.logProviders.LocalCommands[index].indexedCommand = fmt.Sprintf("%s-%d.output", trimmedCmdWithArgs, index)
		}
	}
	return nil
}

func roleMatched(roles []string, DCOSTools DCOSHelper) (bool, error) {
	// if a role is empty, that means it does not matter master or agent, always return true.
	if len(roles) == 0 {
		return true, nil
	}

	myRole, err := DCOSTools.GetNodeRole()
	if err != nil {
		logrus.Errorf("Could not get a node role: %s", err)
		return false, err
	}
	logrus.Debugf("Roles requested: %s, detected: %s", roles, myRole)
	return isInList(myRole, roles), nil
}

func (j *DiagnosticsJob) dispatchLogs(ctx context.Context, provider, entity string, cfg *config.Config, DCOSTools DCOSHelper) (r io.ReadCloser, err error) {
	// make a buffered doneChan to communicate back to process.

	if provider == "units" {
		for _, endpoint := range j.logProviders.HTTPEndpoints {
			if endpoint.FileName == entity {
				canExecute, err := roleMatched(endpoint.Role, DCOSTools)
				if err != nil {
					return r, err
				}
				if !canExecute {
					return r, errors.New("Only DC/OS systemd units are available")
				}
				logrus.Debugf("dispatching a Unit %s", entity)
				r, err = readJournalOutputSince(entity, cfg.FlagDiagnosticsBundleUnitsLogsSinceString)
				return r, err
			}
		}
		return r, fmt.Errorf("%s not found", entity)
	}

	if provider == "files" {
		logrus.Debugf("dispatching a file %s", entity)
		for _, fileProvider := range j.logProviders.LocalFiles {
			if fileProvider.sanitizedLocation == entity {
				canExecute, err := roleMatched(fileProvider.Role, DCOSTools)
				if err != nil {
					return r, err
				}
				if !canExecute {
					return r, errors.New("Not allowed to read a file")
				}
				logrus.Debugf("Found a file %s", fileProvider.Location)
				return readFile(fileProvider.Location)
			}
		}
		return r, errors.New("Not found " + entity)
	}
	if provider == "cmds" {
		logrus.Debugf("dispatching a command %s", entity)
		for _, cmdProvider := range j.logProviders.LocalCommands {
			if entity == cmdProvider.indexedCommand {
				canExecute, err := roleMatched(cmdProvider.Role, DCOSTools)
				if err != nil {
					return r, err
				}
				if !canExecute {
					return r, errors.New("Not allowed to execute a command")
				}
				args := []string{}
				if len(cmdProvider.Command) > 1 {
					args = cmdProvider.Command[1:]
				}

				ce, err := exec.Run(ctx, cmdProvider.Command[0], args)
				if err != nil {
					return nil, err
				}
				return &execCloser{ce}, nil
			}
		}
		return r, errors.New("Not found " + entity)
	}
	return r, errors.New("Unknown provider " + provider)
}

// the summary report is a file added to a zip bundle file to track any errors occured while collection logs.
func updateSummaryReport(preflix string, node Node, error string, r *bytes.Buffer) {
	r.WriteString(fmt.Sprintf("%s [%s] %s %s %s\n", time.Now().String(), preflix, node.IP, node.Role, error))
}

// implement a io.ReadCloser wrapper over dcos/exec
type execCloser struct {
	r io.Reader
}

func (e *execCloser) Read(b []byte) (int, error) {
	return e.r.Read(b)
}

func (e *execCloser) Close() error {
	return nil
}
