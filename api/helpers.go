package api

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/dbus"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	netUrl "net/url"
)

// Requester is an implementation of HTTPRequester interface.
var Requester HTTPRequester = &HTTPReq{}

// DCOSTools is implementation of DCOSHelper interface.
type DCOSTools struct {
	sync.Mutex
	ExhibitorURL string
	ForceTLS     bool
	dcon         *dbus.Conn
	hostname     string
	role         string
	ip           string
	mesosID      string
}

// GetHostname return a localhost hostname.
func (st *DCOSTools) GetHostname() (string, error) {
	if st.hostname != "" {
		return st.hostname, nil
	}
	var err error
	st.hostname, err = os.Hostname()
	if err != nil {
		return "", err
	}
	return st.hostname, nil
}

// DetectIP returns a detected IP by running /opt/mesosphere/bin/detect_ip. It will run only once and cache the result.
// When the function is called again, ip will be taken from cache.
func (st *DCOSTools) DetectIP() (string, error) {
	if st.ip != "" {
		log.Debugf("Found IP in memory: %s", st.ip)
		return st.ip, nil
	}

	var detectIPCmd string
	// Try to get a path to detect_ip script from environment variable.
	// Variable should be available when start 3dt from systemd. Otherwise hardcode the path.
	detectIPCmd = os.Getenv("MESOS_IP_DISCOVERY_COMMAND")
	if detectIPCmd == "" {
		detectIPCmd = "/opt/mesosphere/bin/detect_ip"
	}
	out, err := exec.Command(detectIPCmd).Output()
	st.ip = strings.TrimRight(string(out), "\n")
	if err != nil {
		return st.ip, err
	}
	log.Debugf("Executed /opt/mesosphere/bin/detect_ip, output: %s", st.ip)
	return st.ip, nil
}

// GetNodeRole returns a nodes role. It will run only once and cache the result.
// When the function is called again, ip will be taken from cache.
func (st *DCOSTools) GetNodeRole() (string, error) {
	if st.role != "" {
		return st.role, nil
	}
	if _, err := os.Stat("/etc/mesosphere/roles/master"); err == nil {
		st.role = MasterRole
		return st.role, nil
	}
	if _, err := os.Stat("/etc/mesosphere/roles/slave"); err == nil {
		st.role = AgentRole
		return st.role, nil
	}
	if _, err := os.Stat("/etc/mesosphere/roles/slave_public"); err == nil {
		st.role = AgentPublicRole
		return st.role, nil
	}
	return "", errors.New("Could not determine a role, no /etc/mesosphere/roles/{master,slave,slave_public} file found")
}

// InitializeDBUSConnection opens a dbus connection. The connection is available via st.dcon
func (st *DCOSTools) InitializeDBUSConnection() (err error) {
	// we need to lock the dbus connection for each request
	st.Lock()
	if st.dcon == nil {
		st.dcon, err = dbus.New()
		if err != nil {
			st.Unlock()
			return err
		}
		return nil
	}
	st.Unlock()
	return errors.New("dbus connection is already opened")
}

// CloseDBUSConnection closes a dbus connection.
func (st *DCOSTools) CloseDBUSConnection() error {
	// unlock the dbus connection no matter what
	defer st.Unlock()
	if st.dcon != nil {
		st.dcon.Close()
		// since dbus api does not provide a way to check that the connection is closed, we'd nil it.
		st.dcon = nil
		return nil
	}
	return errors.New("dbus connection is closed")
}

// GetUnitProperties return a map of systemd unit properties received from dbus.
func (st *DCOSTools) GetUnitProperties(pname string) (result map[string]interface{}, err error) {
	// get Service specific properties.
	result, err = st.dcon.GetUnitProperties(pname)
	if err != nil {
		log.Error(err)
		return result, err
	}
	return result, nil
}

// GetUnitNames read a directory /etc/systemd/system/dcos.target.wants and return a list of found systemd units.
func (st *DCOSTools) GetUnitNames() (units []string, err error) {
	files, err := ioutil.ReadDir("/etc/systemd/system/dcos.target.wants")
	if err != nil {
		return units, err
	}
	for _, f := range files {
		units = append(units, f.Name())
	}
	log.Debugf("List of units: %s", units)
	return units, nil
}

// GetJournalOutput returns last 50 lines of journald command output for a specific systemd unit.
func (st *DCOSTools) GetJournalOutput(unit string) (string, error) {
	out, err := exec.Command("journalctl", "--no-pager", "-n", "50", "-u", unit).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func useTLSScheme(url string, use bool) (string, error) {
	if use {
		urlObject, err := netUrl.Parse(url)
		if err != nil {
			return "", err
		}
		urlObject.Scheme = "https"
		return urlObject.String(), nil
	}
	return url, nil
}

// GetMesosNodeID return a mesos node id.
func (st *DCOSTools) GetMesosNodeID() (string, error) {
	if st.mesosID != "" {
		log.Debugf("Found in memory mesos node id: %s", st.mesosID)
		return st.mesosID, nil
	}
	role, err := st.GetNodeRole()
	if err != nil {
		return "", err
	}

	roleMesosPort := make(map[string]int)
	roleMesosPort[MasterRole] = 5050
	roleMesosPort[AgentRole] = 5051
	roleMesosPort[AgentPublicRole] = 5051

	port, ok := roleMesosPort[role]
	if !ok {
		return "", fmt.Errorf("%s role not found", role)
	}
	log.Debugf("using role %s, port %d to get node id", role, port)

	url := fmt.Sprintf("http://%s:%d/state", st.ip, port)
	if err != nil {
		return "", err
	}

	timeout := time.Duration(3) * time.Second
	body, statusCode, err := st.Get(url, timeout)
	if err != nil {
		return "", nil
	}
	if statusCode != http.StatusOK {
		return "", fmt.Errorf("response code: %d", statusCode)
	}

	var respJSON map[string]interface{}
	json.Unmarshal(body, &respJSON)
	if id, ok := respJSON["id"]; ok {
		st.mesosID = id.(string)
		log.Debugf("Received node id %s", st.mesosID)
		return st.mesosID, nil
	}
	return "", errors.New("Field id not found")
}

// Help functions
func isInList(item string, l []string) bool {
	for _, listItem := range l {
		if item == listItem {
			return true
		}
	}
	return false
}

func (st *DCOSTools) doRequest(method, url string, timeout time.Duration, body io.Reader) (responseBody []byte, httpResponseCode int, err error) {
	if url != st.ExhibitorURL {
		url, err = useTLSScheme(url, st.ForceTLS)
		if err != nil {
			return responseBody, http.StatusBadRequest, err
		}
	}

	log.Debugf("[%s] %s, timeout: %s, forceTLS: %s, basicURL: %s", method, url, timeout.String(), st.ForceTLS, url)
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return responseBody, http.StatusBadRequest, err
	}

	resp, err := Requester.Do(request, timeout)
	if err != nil {
		return responseBody, http.StatusBadRequest, err
	}

	defer resp.Body.Close()
	responseBody, err = ioutil.ReadAll(resp.Body)
	return responseBody, resp.StatusCode, nil
}

// Get HTTP request.
func (st *DCOSTools) Get(url string, timeout time.Duration) (body []byte, httpResponseCode int, err error) {
	return st.doRequest("GET", url, timeout, nil)
}

// Post HTTP request.
func (st *DCOSTools) Post(url string, timeout time.Duration) (body []byte, httpResponseCode int, err error) {
	return st.doRequest("POST", url, timeout, nil)
}

// GetTimestamp return time.Now()
func (st *DCOSTools) GetTimestamp() time.Time {
	return time.Now()
}

// GetMasterNodes finds DC/OS masters.
func (st *DCOSTools) GetMasterNodes() (nodesResponse []Node, err error) {
	finder := &findMastersInExhibitor{
		url:   st.ExhibitorURL,
		getFn: st.Get,
		next: &findNodesInDNS{
			forceTLS: st.ForceTLS,
			dnsRecord: "master.mesos",
			role:      MasterRole,
			next:      nil,
		},
	}
	return finder.find()
}

// GetAgentNodes finds DC/OS agents.
func (st *DCOSTools) GetAgentNodes() (nodes []Node, err error) {
	finder := &findNodesInDNS{
		forceTLS: st.ForceTLS,
		dnsRecord: "leader.mesos",
		role:      AgentRole,
		getFn:     st.Get,
		next: &findAgentsInHistoryService{
			pastTime: "/minute/",
			next: &findAgentsInHistoryService{
				pastTime: "/hour/",
				next:     nil,
			},
		},
	}
	return finder.find()
}

// WaitBetweenPulls sleep.
func (st *DCOSTools) WaitBetweenPulls(interval int) {
	time.Sleep(time.Duration(interval) * time.Second)
}

// HTTPReq is an implementation of HTTPRequester interface
type HTTPReq struct {
	caPool *x509.CertPool
}

// Init HTTPReq, prepare CA Pool if file was passed.
func (h *HTTPReq) Init(config *Config, DCOSTools DCOSHelper) error {
	caPool, err := loadCAPool(config)
	if err != nil {
		return err
	}
	h.caPool = caPool
	return nil
}

// Do will do an HTTP/HTTPS request.
func (h *HTTPReq) Do(req *http.Request, timeout time.Duration) (resp *http.Response, err error) {
	headers := make(map[string]string)
	return Do(req, timeout, h.caPool, headers)
}

func loadCAPool(config *Config) (*x509.CertPool, error) {
	// If no ca found, return nil.
	if config.FlagCACertFile == "" {
		return nil, nil
	}

	caPool := x509.NewCertPool()
	f, err := os.Open(config.FlagCACertFile)
	if err != nil {
		return caPool, err
	}
	defer f.Close()

	b, err := ioutil.ReadAll(f)
	if err != nil {
		return caPool, err
	}

	if !caPool.AppendCertsFromPEM(b) {
		return caPool, errors.New("CACertFile parsing failed")
	}
	return caPool, nil
}

// Do makes an HTTP(S) request with predefined http.Request object.
// Caller is responsible for calling http.Response.Body().Close()
func Do(req *http.Request, timeout time.Duration, caPool *x509.CertPool, headers map[string]string) (resp *http.Response, err error) {
	client := http.Client{
		Timeout: timeout,
	}

	// go http client does not copy the headers when it follows the redirect.
	// https://github.com/golang/go/issues/4800
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		for attr, val := range via[0].Header {
			if _, ok := req.Header[attr]; !ok {
				req.Header[attr] = val
			}
		}
		return nil
	}

	if req.URL.Scheme == "https" {
		var tlsClientConfig *tls.Config
		if caPool == nil {
			// do HTTPS without certificate verification.
			tlsClientConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		} else {
			tlsClientConfig = &tls.Config{
				RootCAs: caPool,
			}
		}

		client.Transport = &http.Transport{
			TLSClientConfig: tlsClientConfig,
		}
	}

	// Add headers if available
	for headerKey, headerValue := range headers {
		req.Header.Add(headerKey, headerValue)
	}

	resp, err = client.Do(req)
	if err != nil {
		return resp, err
	}

	// the user of this function is responsible to close the response body.
	return resp, nil
}

// CheckUnitHealth tells if the unit is healthy
func (u *UnitPropertiesResponse) CheckUnitHealth() (int, string, error) {
	if u.LoadState == "" || u.ActiveState == "" || u.SubState == "" {
		return 1, "", fmt.Errorf("LoadState: %s, ActiveState: %s and SubState: %s must be set",
			u.LoadState, u.ActiveState, u.SubState)
	}

	if u.LoadState != "loaded" {
		return 1, fmt.Sprintf("%s is not loaded. Please check `systemctl show all` to check current unit status.", u.ID), nil
	}

	okActiveStates := []string{"active", "inactive", "activating"}
	if !isInList(u.ActiveState, okActiveStates) {
		return 1, fmt.Sprintf(
			"%s state is not one of the possible states %s. Current state is [ %s ]. " +
			"Please check `systemctl show all %s` to check current unit state. ", u.ID, okActiveStates, u.ActiveState, u.ID), nil
	}

	// https://www.freedesktop.org/wiki/Software/systemd/dbus/
	// if a unit is in `activating` state and `auto-restart` sub-state it means unit is trying to start and fails.
	if u.ActiveState == "activating" && u.SubState == "auto-restart" {
		// If ActiveEnterTimestampMonotonic is 0, it means that unit has never been able to switch to active state.
		// Most likely a ExecStartPre fails before the unit can execute ExecStart.
		if u.ActiveEnterTimestampMonotonic == 0 {
			return 1, fmt.Sprintf("unit %s has never entered `active` state", u.ID), nil
		}

		// If InactiveEnterTimestampMonotonic > ActiveEnterTimestampMonotonic that means that a unit was active
		// some time ago, but then something happened and it cannot restart.
		if u.InactiveEnterTimestampMonotonic > u.ActiveEnterTimestampMonotonic {
			return 1, fmt.Sprintf("unit %s is flapping. Please check `systemctl status %s` to check current unit state.", u.ID, u.ID), nil
		}
	}

	return 0, "", nil
}

func normalizeProperty(unitProps map[string]interface{}, dt Dt) (healthResponseValues, error) {
	var (
		description, prettyName string
		propsResponse UnitPropertiesResponse
	)


	marshaledPropsResponse, err := json.Marshal(unitProps)
	if err != nil {
		return healthResponseValues{}, err
	}

	if err = json.Unmarshal(marshaledPropsResponse, &propsResponse); err != nil {
		return healthResponseValues{}, err
	}

	unitHealth, unitOutput, err := propsResponse.CheckUnitHealth()
	if err != nil {
		return healthResponseValues{}, err
	}

	if unitHealth > 0 {
		journalOutput, err := dt.DtDCOSTools.GetJournalOutput(propsResponse.ID)
		if err == nil {
			unitOutput += "\n"
			unitOutput += journalOutput
		} else {
			log.Error(err)
		}
	}

	s := strings.Split(propsResponse.Description, ": ")
	if len(s) != 2 {
		description = strings.Join(s, " ")

	} else {
		prettyName, description = s[0], s[1]
	}

	return healthResponseValues{
		UnitID:     propsResponse.ID,
		UnitHealth: unitHealth,
		UnitOutput: unitOutput,
		UnitTitle:  description,
		Help:       "",
		PrettyName: prettyName,
	}, nil
}

type stdoutTimeoutPipe struct {
	stdoutPipe io.ReadCloser
	stderrPipe io.ReadCloser
	cmd        *exec.Cmd
	done       chan struct{}
}

func (cm *stdoutTimeoutPipe) Read(p []byte) (n int, err error) {
	n, err = cm.stdoutPipe.Read(p)
	if n == 0 {
		log.Error("Coult not read stdout, trying to read stderr")
		n, err = cm.stderrPipe.Read(p)
	}
	return
}

func (cm *stdoutTimeoutPipe) Close() error {
	select {
	case <-cm.done:
		return nil
	default:
		close(cm.done)
		if cm.cmd != nil {
			if err := cm.cmd.Wait(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (cm *stdoutTimeoutPipe) kill() error {
	if cm.cmd != nil {
		cm.cmd.Process.Kill()
	}
	return nil
}

// Run a command. The Wait() will be called only if the caller closes done channel or timeout occurs.
// This will make sure we can read from StdoutPipe.
func runCmd(command []string, timeout int) (io.ReadCloser, error) {
	stdout := &stdoutTimeoutPipe{}
	stdout.done = make(chan struct{})
	// if command has arguments, append them to args.
	args := []string{}
	if len(command) > 1 {
		args = command[1:len(command)]
	}
	log.Debugf("Run: %s", command)
	cmd := exec.Command(command[0], args...)

	var err error
	// get stdout pipe
	stdout.stdoutPipe, err = cmd.StdoutPipe()
	if err != nil {
		return stdout, err
	}

	// ignore and log error if stderr failed, but do not fail
	stdout.stderrPipe, err = cmd.StderrPipe()
	if err != nil {
		log.Error(err)
	}

	// Execute a command
	if err := cmd.Start(); err != nil {
		return stdout, err
	}
	stdout.cmd = cmd

	// Run a separate goroutine to handle timeout and read command's return code.
	go func() {
		fullCommand := strings.Join(cmd.Args, " ")
		select {
		case <-stdout.done:
			log.Infof("Command %s executed successfully, PID %d", fullCommand, stdout.cmd.Process.Pid)
		case <-time.After(time.Duration(timeout) * time.Second):
			log.Errorf("Timeout occured, command %s, killing PID %d", fullCommand, cmd.Process.Pid)
			stdout.kill()
		}
	}()
	return stdout, nil
}

// open a file for reading, a caller if responsible to close a file descriptor.
func readFile(fileLocation string) (r io.ReadCloser, err error) {
	file, err := os.Open(fileLocation)
	if err != nil {
		return r, err
	}
	return file, nil
}

func readJournalOutputSince(unit, sinceString string, timeout int) (io.ReadCloser, error) {
	stdout := &stdoutTimeoutPipe{}
	if !strings.HasPrefix(unit, "dcos-") {
		return stdout, errors.New("Unit should start with dcos-, got: " + unit)
	}
	if strings.ContainsAny(unit, " ;&|") {
		return stdout, errors.New("Unit cannot contain special charachters or spaces")
	}
	command := []string{"journalctl", "--no-pager", "-u", unit, "--since", sinceString}
	return runCmd(command, timeout)
}
