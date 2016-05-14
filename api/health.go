package api

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/dbus"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Global health report variable
var GlobalHealthReport UnitsHealth

type UnitsHealth struct {
	sync.Mutex
	healthReport UnitsHealthResponseJsonStruct
}

func (uh *UnitsHealth) GetHealthReport() UnitsHealthResponseJsonStruct {
	uh.Lock()
	defer uh.Unlock()
	return uh.healthReport
}

func (uh *UnitsHealth) UpdateHealthReport(healthReport UnitsHealthResponseJsonStruct) {
	uh.Lock()
	defer uh.Unlock()
	uh.healthReport = healthReport
}

// start updating health report
func StartUpdateHealthReport(config Config, readyChan chan bool, runOnce bool) {
	var ready bool
	for {
		healthReport, err := GetUnitsProperties(&config)
		if err == nil {
			if ready == false {
				readyChan <- true
				ready = true
			}
			GlobalHealthReport.UpdateHealthReport(healthReport)
		} else {
			log.Error("Could not update systemd units health report")
			log.Error(err)
		}
		if runOnce {
			log.Debug("Run startUpdateHealthReport only once")
			return
		}
		time.Sleep(time.Second * 60)
	}
}

// HealthReporter implementation
type DcosHealth struct {
	sync.Mutex
	dcon     *dbus.Conn
	hostname string
	role     string
	ip       string
	mesos_id string
}

func (st *DcosHealth) GetHostname() string {
	if st.hostname != "" {
		return st.hostname
	}
	var err error
	st.hostname, err = os.Hostname()
	if err != nil {
		log.Error(err)
		st.hostname = "UnknownHostname"
	}
	return st.hostname
}

func (st *DcosHealth) DetectIp() string {
	if st.ip != "" {
		log.Debugf("Found IP in memory: %s", st.ip)
		return st.ip
	}

	var detect_ip_cmd string
	// Try to get a path to detect_ip script from environment variable.
	// Variable should be available when start 3dt from systemd. Otherwise hardcode the path.
	detect_ip_cmd = os.Getenv("MESOS_IP_DISCOVERY_COMMAND")
	if detect_ip_cmd == "" {
		detect_ip_cmd = "/opt/mesosphere/bin/detect_ip"
	}
	out, err := exec.Command(detect_ip_cmd).Output()
	st.ip = strings.TrimRight(string(out), "\n")
	if err != nil {
		log.Error(err)
		return st.ip
	}
	log.Debugf("Executed /opt/mesosphere/bin/detect_ip, output: %s", st.ip)
	return st.ip
}

// detect node role
func (st *DcosHealth) GetNodeRole() string {
	if st.role != "" {
		return st.role
	}
	if _, err := os.Stat("/etc/mesosphere/roles/master"); err == nil {
		st.role = "master"
		return st.role
	}
	if _, err := os.Stat("/etc/mesosphere/roles/slave"); err == nil {
		st.role = "agent"
		return st.role
	}
	return ""
}

func (st *DcosHealth) InitializeDbusConnection() (err error) {
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

func (st *DcosHealth) CloseDbusConnection() error {
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

func (st *DcosHealth) GetUnitProperties(pname string) (result map[string]interface{}, err error) {
	// get Service specific properties.
	result, err = st.dcon.GetUnitProperties(pname)
	if err != nil {
		log.Error(err)
		return result, err
	}
	return result, nil
}

func (st *DcosHealth) GetUnitNames() (units []string, err error) {
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

func (st *DcosHealth) GetJournalOutput(unit string) (string, error) {
	out, err := exec.Command("journalctl", "--no-pager", "-n", "50", "-u", unit).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (st *DcosHealth) GetMesosNodeId(role string, field string) string {
	if st.mesos_id != "" {
		log.Debugf("Found in memory mesos node id: %s", st.mesos_id)
		return st.mesos_id
	}

	var port int
	if role == "master" {
		port = 5050
	} else {
		port = 5051
	}
	log.Debugf("using role %s, port %d to get node id", role, port)

	var url string = fmt.Sprintf("http://%s:%d/state", st.ip, port)

	log.Debugf("GET %s", url)
	resp, err := http.Get(url)
	if err != nil {
		log.Errorf("Could not connect to %s", url)
		return ""
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	var respJson map[string]interface{}
	json.Unmarshal(body, &respJson)
	if id, ok := respJson[field]; ok {
		st.mesos_id = id.(string)
		log.Debugf("Received node id %s", st.mesos_id)
		return st.mesos_id
	}
	log.Errorf("Could not unmarshal json response, field: %s", field)
	return ""
}

// Help functions
func IsInList(item string, l []string) bool {
	for _, list_item := range l {
		if item == list_item {
			return true
		}
	}
	return false
}

func NormalizeProperty(unitName string, p map[string]interface{}, si HealthReporter) UnitHealthResponseFieldsStruct {
	var unitHealth int = 0
	var unitOutput string

	// check keys
	log.Debugf("%s LoadState: %s", unitName, p["LoadState"])
	if p["LoadState"] != "loaded" {
		unitHealth = 1
		unitOutput += fmt.Sprintf("%s is not loaded. Please check `systemctl show all` to check current unit status. ", unitName)
	}

	okStates := []string{"active", "inactive", "activating"}
	log.Debugf("%s ActiveState: %s", unitName, p["ActiveState"])
	if !IsInList(p["ActiveState"].(string), okStates) {
		unitHealth = 1
		unitOutput += fmt.Sprintf("%s state is not one of the possible states %s. Current state is [ %s ]. Please check `systemctl show all %s` to check current unit state. ", unitName, okStates, p["ActiveState"], unitName)
	}

	if unitHealth > 0 {
		journalOutput, err := si.GetJournalOutput(unitName)
		if err == nil {
			unitOutput += "\n"
			unitOutput += journalOutput
		} else {
			log.Error(err)
		}
	}

	var prettyName, description string
	s := strings.Split(p["Description"].(string), ": ")
	if len(s) != 2 {
		description = strings.Join(s, " ")

	} else {
		prettyName, description = s[0], s[1]
	}

	return UnitHealthResponseFieldsStruct{
		UnitId:     unitName,
		UnitHealth: unitHealth,
		UnitOutput: unitOutput,
		UnitTitle:  description,
		Help:       "",
		PrettyName: prettyName,
	}
}

// endpoint "/api/v1/system/health"
func GetUnitsProperties(config *Config) (UnitsHealthResponseJsonStruct, error) {
	// detect DC/OS systemd units
	foundUnits, err := config.HealthReport.GetUnitNames()
	if err != nil {
		log.Error(err)
	}
	var allUnitsProperties []UnitHealthResponseFieldsStruct
	// open dbus connection
	if err = config.HealthReport.InitializeDbusConnection(); err != nil {
		return UnitsHealthResponseJsonStruct{}, err
	}
	log.Debug("Opened dbus connection")

	// DCOS-5862 blacklist systemd units
	excludeUnits := []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}

	units := append(config.SystemdUnits, foundUnits...)
	for _, unit := range units {
		if IsInList(unit, excludeUnits) {
			log.Debugf("Skipping blacklisted systemd unit %s", unit)
			continue
		}
		currentProperty, err := config.HealthReport.GetUnitProperties(unit)
		if err != nil {
			log.Errorf("Could not get properties for unit: %s", unit)
			continue
		}
		allUnitsProperties = append(allUnitsProperties, NormalizeProperty(unit, currentProperty, config.HealthReport))
	}
	// after we finished querying systemd units, close dbus connection
	if err = config.HealthReport.CloseDbusConnection(); err != nil {
		// we should probably return here, since we cannot guarantee that all units have been queried.
		return UnitsHealthResponseJsonStruct{}, err
	}
	return UnitsHealthResponseJsonStruct{
		Array:       allUnitsProperties,
		Hostname:    config.HealthReport.GetHostname(),
		IpAddress:   config.HealthReport.DetectIp(),
		DcosVersion: config.DcosVersion,
		Role:        config.HealthReport.GetNodeRole(),
		MesosId:     config.HealthReport.GetMesosNodeId(config.HealthReport.GetNodeRole(), "id"),
		TdtVersion:  config.Version,
	}, nil
}
