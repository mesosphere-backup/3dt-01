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
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"io/ioutil"
)

// snapshotJob
type SnapshotJob struct {
	sync.Mutex
	cancelChan    chan bool

	Running          bool          `json:"is_running"`
	Status           string        `json:"status"`
	Errors           []string      `json:"errors"`
	LastSnapshotPath string        `json:"last_snapshot_dir"`
	JobStarted       time.Time     `json:"job_started"`
	JobEnded         time.Time     `json:"job_ended"`
	JobDuration      time.Duration `json:"job_duration"`
}

type snapshotReportResponse struct {
	Version int      `json:"version"`
	Status  string   `json:"status"`
	Errors  []string `json:"errors"`
}

type snapshotReportStatus struct {
	// job related fields
	Running                              bool     `json:"is_running"`
	Status                               string   `json:"status"`
	Errors                               []string `json:"errors"`
	LastSnapshotPath                      string   `json:"last_snapshot_dir"`
	JobStarted                            string   `json:"job_started"`
	JobEnded                              string   `json:"job_ended"`
	JobDuration                           string   `json:"job_duration"`

	// config related fields
	SnapshotBaseDir                       string `json:"snapshot_dir"`
	SnapshotJobTimeoutMin                 int    `json:"snapshot_job_timeout_min"`
	SnapshotUnitsLogsSinceHours           string `json:"journald_logs_since_hours"`
	SnapshotJobGetSingleUrlTimeoutMinutes int `json:"snapshot_job_get_since_url_timeout_min"`
	CommandExecTimeoutSec                 int `json:"command_exec_timeout_sec"`

	// metrics related
	DiskUsedPercent                       float64 `json:"snapshot_partition_disk_usage_percent"`
}

func (j *SnapshotJob) getHttpAddToZip(node Node, endpoints map[string]string, folder string, zipWriter *zip.Writer, summaryReport *bytes.Buffer, config *Config) error {
	for fileName, httpEndpoint := range endpoints {
		full_url := "http://"+node.Ip+httpEndpoint
		log.Debugf("GET %s", full_url)
		j.Status = "GET " + full_url
		client := new(http.Client)
		client.Timeout = time.Duration(time.Minute*time.Duration(config.FlagSnapshotJobGetSingleUrlTimeoutMinutes))
		request, err := http.NewRequest("GET", full_url, nil)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			log.Error(err)
			updateSummaryReport(fmt.Sprintf("could not create request for url: %s", full_url), node, err.Error(), summaryReport)
			continue
		}
		request.Header.Add("Accept-Encoding", "gzip")
		resp, err := client.Do(request)
		if err != nil {
			j.Errors = append(j.Errors, err.Error())
			log.Errorf("Could not fetch url: %s", full_url)
			log.Error(err)
			updateSummaryReport(fmt.Sprintf("could not fetch url: %s", full_url), node, err.Error(), summaryReport)
			continue
		}
		if resp.Header.Get("Content-Encoding") == "gzip" {
			fileName += ".gz"
		}

		// put all logs in a `ip_role` folder
		zipFile, err := zipWriter.Create(filepath.Join(node.Ip+"_"+node.Role, fileName))
		if err != nil {
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

func (j *SnapshotJob) getStatusAll(config *Config, puller Puller) (map[string]snapshotReportStatus, error) {
	statuses := make(map[string]snapshotReportStatus)

	masterNodes, err := puller.LookupMaster()
	if err != nil {
		return statuses, err
	}

	for _, master := range masterNodes {
		var status snapshotReportStatus
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/status", master.Ip, config.FlagPort, BaseRoute)
		body, statusCode, err := puller.GetHttp(url)
		if err = json.Unmarshal(body, &status); err != nil {
			log.Errorf("GET %s failed, status code: %d", url, statusCode)
			log.Error(err)
			continue
		}
		statuses[master.Ip] = status
	}
	return statuses, nil
}

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

		SnapshotBaseDir:       config.FlagSnapshotDir,
		SnapshotJobTimeoutMin: config.FlagSnapshotJobTimeoutMinutes,
		SnapshotJobGetSingleUrlTimeoutMinutes: config.FlagSnapshotJobGetSingleUrlTimeoutMinutes,
		SnapshotUnitsLogsSinceHours: config.FlagSnapshotUnitsLogsSinceHours,
		CommandExecTimeoutSec: config.FlagCommandExecTimeoutSec,


		DiskUsedPercent:       used,
	}
}

func (j *SnapshotJob) start() {
	j.Running = true
}

func (j *SnapshotJob) stop() {
	j.Running = false
}

func (j *SnapshotJob) isSnapshotAvailable(snapshotName string, config *Config, puller Puller) (string, string, bool, error) {
	snapshots, err := listAllSnapshots(config, puller)
	if err != nil {
		return "", "", false, err
	}
	log.Infof("Trying to find a snapshot %s on remote hosts", snapshotName)
	for host, remoteSnapshots := range snapshots {
		for _, remoteSnapshot := range remoteSnapshots {
			if snapshotName == path.Base(remoteSnapshot) {
				log.Infof("Snapshot %s found on a host: %s", snapshotName, host)
				return host, remoteSnapshot, true, nil
			}
		}
	}
	return "", "", false, nil
}

//
func (j *SnapshotJob) DeleteSnapshot(snapshotName string, config *Config) error {
	if !strings.HasPrefix(snapshotName, "snapshot-") || !strings.HasSuffix(snapshotName, ".zip") {
		return errors.New("format allowed  snapshot-*.zip")
	}
	snapshotPath := path.Join(config.FlagSnapshotDir, snapshotName)
	log.Debugf("Trying remove snapshot: %s", snapshotPath)
	_, err := os.Stat(snapshotPath)
	if err != nil {
		return err
	}
	if err = os.Remove(snapshotPath); err != nil {
		return err
	}
	log.Infof("%s deleted", snapshotPath)
	return nil
}

func (j *SnapshotJob) run(req snapshotCreateRequest, config *Config, puller Puller, dcosHealth HealthReporter) error {
	if dcosHealth.GetNodeRole() == "agent" {
		return errors.New("Running snapshot job on agent node is not implemented.")
	}

	if j.Running {
		return errors.New("Job is already running")
	}
	_, err := os.Stat(config.FlagSnapshotDir)
	if os.IsNotExist(err) {
		log.Infof("snapshot dir: %s not found, attempting to create one", config.FlagSnapshotDir)
		if err := os.Mkdir(config.FlagSnapshotDir, os.ModePerm); err != nil {
			errMsg := fmt.Sprintf("Could not create snapshot directory: %s", config.FlagSnapshotDir)
			j.Errors = append(j.Errors, errMsg)
			j.Status = "Job failed"
			return errors.New(errMsg)
		}
	}

	t := time.Now()
	j.LastSnapshotPath = fmt.Sprintf("%s/snapshot-%d-%02d-%02dT%02d:%02d:%02d-%d.zip", config.FlagSnapshotDir, t.Year(),
		t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond())
	j.Status = "Snapshot job started, archive will be available: " + j.LastSnapshotPath

	// first discover all nodes in a cluster, then try to find requested nodes.
	masterNodes, err := puller.LookupMaster()
	if err != nil {
		return err
	}
	agentNodes, err := puller.GetAgentsFromMaster()
	if err != nil {
		return err
	}
	foundNodes, err := findRequestedNodes(masterNodes, agentNodes, req.Nodes)
	if err != nil {
		return err
	}
	log.Debugf("Found requested nodes: %s", foundNodes)

	j.cancelChan = make(chan bool)
	go j.runBackgroundReport(foundNodes, config, puller)
	return nil
}

func updateSummaryReport(preflix string, node Node, error string, r *bytes.Buffer) {
	r.WriteString(fmt.Sprintf("%s [%s] %s %s %s\n", time.Now().String(), preflix, node.Ip, node.Role, error))
}

func (j *SnapshotJob) runBackgroundReport(nodes []Node, config *Config, puller Puller) {
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

	// makesure we always cancel a timeout gorutine when the report is finished.
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
		url := fmt.Sprintf("http://%s:%d%s/logs", node.Ip, config.FlagPort, BaseRoute)
		select {
		case _, ok := <-j.cancelChan:
			if ok {
				j.Status = "Job has been canceled"
				j.Errors = append(j.Errors, j.Status)
				log.Debug(j.Status)
				os.Remove(zipfile.Name())
				j.LastSnapshotPath = ""
				return
			} else {
				errMsg := "cancelChan is closed!"
				j.Errors = append(j.Errors, errMsg)
				return
			}
		default:
			j.Status = fmt.Sprintf("Collecting from a node: %s, url: %s", node.Ip, url)
			log.Debug(j.Status)
		}

		endpoints := make(map[string]string)
		body, statusCode, err := puller.GetHttp(url)
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
		err = j.getHttpAddToZip(node, endpoints, j.LastSnapshotPath, zipWriter, summaryErrorsReport, config)
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
			if requestedNode == clusterNode.Ip || requestedNode == clusterNode.MesosId || requestedNode == clusterNode.Host {
				matchedNodes = append(matchedNodes, clusterNode)
			}
		}
	}
	if len(matchedNodes) > 0 {
		return matchedNodes, nil
	}
	return matchedNodes, errors.New(fmt.Sprintf("Requested nodes: %s not found", requestedNodes))
}

func (j *SnapshotJob) cancel(dcosHealth HealthReporter) error {
	if dcosHealth.GetNodeRole() == "agent" {
		return errors.New("Canceling snapshot job on agent node is not implemented.")
	}

	if !j.Running {
		return errors.New("Job is not running")
	}
	log.Debug("Cancelling job")
	j.cancelChan <- true
	return nil
}

func dispatchLogs(provider string, entity string, config *Config, healthReport HealthReporter) (doneChan chan bool, r io.ReadCloser, err error) {
	// make a buffered doneChan to communicate back to process.
	doneChan = make(chan bool, 1)
	intProviders, err := loadInternalProviders(config, healthReport)
	if err != nil {
		log.Error(err)
	}
	extProviders, err := loadExternalProviders(config, healthReport)
	if err != nil {
		log.Error(err)
	}
	if provider == "units" {
		log.Debugf("dispatching a unit %s", entity)
		r, err = readJournalOutput(entity, config, doneChan)
		return doneChan, r, err
	}
	if provider == "files" {
		log.Debugf("dispatching a file %s", entity)
		for _, fileProvider := range append(intProviders.LocalFiles, extProviders.LocalFiles...) {
			if filepath.Base(fileProvider.Location) == entity {
				log.Debugf("Found a file %s", fileProvider.Location)
				r, err = readFile(fileProvider.Location)
				return doneChan, r, err
			}
		}
		return doneChan, r, errors.New("Not found "+entity)
	}
	if provider == "cmds" {
		log.Debugf("dispatching a command %s", entity)
		for _, cmdProvider := range append(intProviders.LocalCommands, extProviders.LocalCommands...) {
			if len(cmdProvider.Command) > 0 {
				if entity == filepath.Base(cmdProvider.Command[0]) {
					r, err = runCmd(cmdProvider.Command, doneChan, config.FlagCommandExecTimeoutSec)
					return doneChan, r, err
				}
			}
		}
		return doneChan, r, errors.New("Not found "+entity)
	}
	return doneChan, r, errors.New("Unknown provider "+provider)
}

func runCmd(command []string, doneChan chan bool, timeout int) (r io.ReadCloser, err error) {
	args := []string{}
	if len(command) > 1 {
		args = command[1:len(command)]
	}
	log.Debugf("Run: %s", command)
	cmd := exec.Command(command[0], args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return r, err
	}
	if err := cmd.Start(); err != nil {
		return r, err
	}
	go func(p *os.Process, doneChan chan bool, timeout int) {
		for {
			select {
			case <- doneChan:
				log.Debugf("Process finished %d", p.Pid)
				// always call Wait to avoid zombies
				p.Wait()
				return
			case <-time.After(time.Second*time.Duration(timeout)):
				log.Errorf("Timeout exceeded for process, killing %d", p.Pid)
				p.Kill()
				p.Wait()
				return
			}
		}
	}(cmd.Process, doneChan, timeout)
	return stdout, nil
}

func readFile(fileLocation string) (r io.ReadCloser, err error) {
	file, err := os.Open(fileLocation)
	if err != nil {
		return r, err
	}
	return file, nil
}

func readJournalOutput(unit string, config *Config, doneChan chan bool) (r io.ReadCloser, err error) {
	if !strings.HasPrefix(unit, "dcos-") {
		return r, errors.New("Unit should start with dcos-, got: "+unit)
	}
	if strings.ContainsAny(unit, " ;&|") {
		return r, errors.New("Unit cannot contain special charachters or spaces")
	}
	command := []string{"journalctl", "--no-pager", "-u", unit, "--since", config.FlagSnapshotUnitsLogsSinceHours+" hours ago"}
	return runCmd(command, doneChan, config.FlagCommandExecTimeoutSec)
}

type LogProviders struct {
	HTTPEndpoints []HttpProvider
	LocalFiles    []FileProvider
	LocalCommands []CommandProvider
}

type HttpProvider struct {
	Port     int
	Uri      string
	FileName string
	Role     string
}

type FileProvider struct {
	Location string
	Role     string
}

type CommandProvider struct {
	Command []string
	Role     string
}

// get a list of all available endpoints
func getLogsEndpointList(config *Config, dcosHealth HealthReporter) (endpoints map[string]string, err error) {
	internalProviders, err := loadInternalProviders(config, dcosHealth)
	if err != nil {
		log.Error(err)
	}
	externalProviders, err := loadExternalProviders(config, dcosHealth)
	if err != nil {
		log.Error(err)
	}

	endpoints = make(map[string]string)
	// Load HTTP endpoints
	for _, endpoint := range append(internalProviders.HTTPEndpoints, externalProviders.HTTPEndpoints...) {
		// if role is set and does not equal current role, skip.
		if endpoint.Role != "" && endpoint.Role != dcosHealth.GetNodeRole() {
			continue
		}
		endpoints[endpoint.FileName] = fmt.Sprintf(":%d%s", endpoint.Port, endpoint.Uri)
	}

	// Load file endpoints
	for _, file := range append(internalProviders.LocalFiles, externalProviders.LocalFiles...) {
		// if role is set and does not equal current role, skip.
		if file.Role != "" && file.Role != dcosHealth.GetNodeRole() {
			continue
		}
		endpoints[file.Location] = fmt.Sprintf(":%d%s/logs/files/%s", config.FlagPort, BaseRoute, filepath.Base(file.Location))
	}

	// Load command endpoints
	for _, c := range append(internalProviders.LocalCommands, externalProviders.LocalCommands...) {
		// if role is set and does not equal current role, skip.
		if c.Role != "" && c.Role != dcosHealth.GetNodeRole() {
			continue
		}
		if len(c.Command) > 0 {
			endpoints[filepath.Base(c.Command[0])+".output"] = fmt.Sprintf(":%d%s/logs/cmds/%s", config.FlagPort, BaseRoute, filepath.Base(c.Command[0]))
		}
	}
	return endpoints, nil
}

func loadExternalProviders(config *Config, dcosHealth HealthReporter) (LogProviders, error) {
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

func loadInternalProviders(config *Config, dcosHealth HealthReporter) (internalConfigProviders LogProviders, err error) {
	units, err := dcosHealth.GetUnitNames()
	if err != nil {
		return
	}

	// load default HTTP
	var httpEndpoints []HttpProvider
	for _, unit := range append(units, config.SystemdUnits...) {
		httpEndpoints = append(httpEndpoints, HttpProvider{
			Port:     config.FlagPort,
			Uri:      fmt.Sprintf("%s/logs/units/%s", BaseRoute, unit),
			FileName: unit + ".log",
		})
	}
	httpEndpoints = append(httpEndpoints, HttpProvider{
		Port: 1050,
		Uri: BaseRoute,
		FileName: "3dt-health.log",
	})

	return LogProviders{
		HTTPEndpoints: httpEndpoints,
	}, nil
}

func listAllSnapshots(config *Config, puller Puller) (map[string][]string, error) {
	collectedSnapshots := make(map[string][]string)
	masterNodes, err := puller.LookupMaster()
	if err != nil {
		return collectedSnapshots, err
	}
	for _, master := range masterNodes {
		var snapshotUrls []string
		url := fmt.Sprintf("http://%s:%d%s/report/snapshot/list", master.Ip, config.FlagPort, BaseRoute)
		body, _, err := puller.GetHttp(url)
		if err != nil {
			log.Error(err)
			continue
		}
		if err = json.Unmarshal(body, &snapshotUrls); err != nil {
			log.Error(err)
			continue
		}
		collectedSnapshots[fmt.Sprintf("%s:%d", master.Ip, config.FlagPort)] = snapshotUrls
	}
	return collectedSnapshots, nil
}

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
