package api

import (
	"encoding/json"
	"errors"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/coreos/go-systemd/dbus"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
	"bytes"
	"io"
)

// Global health report variable
var GlobalUnitsHealthReport UnitsHealth

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

func StartUpdateHealthReport(config *Config, dcosHealth HealthReporter, readyChan chan bool, runOnce bool) {
	var ready bool
	for {
		healthReport, err := GetUnitsProperties(config, dcosHealth)
		if err == nil {
			if ready == false {
				readyChan <- true
				ready = true
			}
		} else {
			log.Error("Could not update systemd units health report")
			log.Error(err)
		}
		GlobalUnitsHealthReport.UpdateHealthReport(healthReport)
		if runOnce {
			log.Debug("Run startUpdateHealthReport only once")
			return
		}
		time.Sleep(time.Second * 60)
	}
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

func (st *DcosHealth) GetJournalOutput(unit string, timeout int) (string, error) {
	if strings.ContainsAny(unit, " ;&|") {
		return "", errors.New("Unit name canot contain special charachters or space. Got "+unit)
	}
	command := []string{"journalctl", "--no-pager", "-n", "50", "-u", unit}
	doneChan := make(chan bool)
	r, err := runCmd(command, doneChan, timeout)
	if err != nil {
		return "", err
	}
	buffer := new(bytes.Buffer)
	io.Copy(buffer, r)
	doneChan <- true
	return buffer.String(), nil
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

func NormalizeProperty(unitName string, p map[string]interface{}, si HealthReporter, config *Config) UnitHealthResponseFieldsStruct {
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
		journalOutput, err := si.GetJournalOutput(unitName, config.FlagCommandExecTimeoutSec)
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

func updateSystemMetrics() (sysMetrics SysMetrics, err error) {
	var globalError string
	v, err := mem.VirtualMemory()
	if err == nil {
		sysMetrics.Memory = *v
	} else {
		globalError += err.Error()
	}

	la, err := load.Avg()
	if err == nil {
		sysMetrics.LoadAvarage = *la
	} else {
		globalError += err.Error()
	}
	d, err := disk.Partitions(true)
	if err == nil {
		sysMetrics.Partitions = d
	} else {
		globalError += err.Error()
		return sysMetrics, errors.New(globalError)
	}

	var diskUsage []disk.UsageStat
	for _, diskProps := range d {
		currentDiskUsage, err := disk.Usage(diskProps.Mountpoint)
		if err != nil {
			log.Error(err)
			continue
		}
		if currentDiskUsage.String() == "" {
			continue
		}
		diskUsage = append(diskUsage, *currentDiskUsage)
	}
	sysMetrics.DiskUsage = diskUsage
	return sysMetrics, nil
}

// endpoint "/api/v1/system/health"
func GetUnitsProperties(config *Config, dcosHealth HealthReporter) (healthReport UnitsHealthResponseJsonStruct, err error) {
	// update system metrics first to make sure we always return them.
	sysMetrics, err := updateSystemMetrics()
	if err != nil {
		log.Error(err)
	}
	healthReport.System = sysMetrics

	// detect DC/OS systemd units
	foundUnits, err := dcosHealth.GetUnitNames()
	if err != nil {
		log.Error(err)
	}
	var allUnitsProperties []UnitHealthResponseFieldsStruct
	// open dbus connection
	if err = dcosHealth.InitializeDbusConnection(); err != nil {
		return healthReport, err
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
		currentProperty, err := dcosHealth.GetUnitProperties(unit)
		if err != nil {
			log.Errorf("Could not get properties for unit: %s", unit)
			continue
		}
		allUnitsProperties = append(allUnitsProperties, NormalizeProperty(unit, currentProperty, dcosHealth, config))
	}
	// after we finished querying systemd units, close dbus connection
	if err = dcosHealth.CloseDbusConnection(); err != nil {
		// we should probably return here, since we cannot guarantee that all units have been queried.
		return healthReport, err
	}
	return UnitsHealthResponseJsonStruct{
		Array:       allUnitsProperties,
		System:      sysMetrics,
		Hostname:    dcosHealth.GetHostname(),
		IpAddress:   dcosHealth.DetectIp(),
		DcosVersion: config.DcosVersion,
		Role:        dcosHealth.GetNodeRole(),
		MesosId:     dcosHealth.GetMesosNodeId(dcosHealth.GetNodeRole(), "id"),
		TdtVersion:  config.Version,
	}, nil
}
