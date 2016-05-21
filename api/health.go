package api

import (
	log "github.com/Sirupsen/logrus"

	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"

	"sync"
	"time"
)

// unitsHealthReport gets updated in a separate goroutine every 60 sec This variable contains a status of a DC/OS
// systemd units for the past 60 sec.
var unitsHealthReport unitsHealth

// unitsHealth is a container for global health report. Getting and updating of health report must go through
// GetHealthReport and UpdateHealthReport this allows to access data in concurrent manner.
type unitsHealth struct {
	sync.Mutex
	healthReport UnitsHealthResponseJSONStruct
}

// getHealthReport returns a global health report of UnitsHealthResponseJsonStruct type.
func (uh *unitsHealth) GetHealthReport() UnitsHealthResponseJSONStruct {
	uh.Lock()
	defer uh.Unlock()
	return uh.healthReport
}

// updateHealthReport updates a global health report of UnitsHealthResponseJsonStruct type.
func (uh *unitsHealth) UpdateHealthReport(healthReport UnitsHealthResponseJSONStruct) {
	uh.Lock()
	defer uh.Unlock()
	uh.healthReport = healthReport
}

// StartUpdateHealthReport should be started in a separate goroutine to update global health report periodically.
func StartUpdateHealthReport(config Config, readyChan chan struct{}, runOnce bool) {
	var closedReadyChan bool
	for {
		healthReport, err := GetUnitsProperties(&config)
		if err == nil {
			if !closedReadyChan {
				close(readyChan)
				closedReadyChan = true
			}
		} else {
			log.Error("Could not update systemd units health report")
			log.Error(err)
		}
		unitsHealthReport.UpdateHealthReport(healthReport)
		if runOnce {
			log.Debug("Run startUpdateHealthReport only once")
			return
		}
		time.Sleep(time.Duration(config.FlagUpdateHealthReportInterval) * time.Second)
	}
}

func getHostVirtualMemory() (mem.VirtualMemoryStat, error) {
	vmem, err := mem.VirtualMemory()
	if err != nil {
		return *vmem, err
	}
	return *vmem, nil
}

func getHostLoadAvarage() (load.AvgStat, error) {
	la, err := load.Avg()
	if err != nil {
		return *la, err
	}
	return *la, nil
}

func getHostDiskPartitions(all bool) (diskPartitions []disk.PartitionStat, err error) {
	diskPartitions, err = disk.Partitions(all)
	if err != nil {
		return diskPartitions, err
	}
	return diskPartitions, nil
}

func getHostDiskUsage(diskPartitions []disk.PartitionStat) (diskUsage []disk.UsageStat, err error) {
	for _, diskProps := range diskPartitions {
		currentDiskUsage, err := disk.Usage(diskProps.Mountpoint)
		if err != nil {
			// Just log the error, do not return.
			log.Error(err)
			continue
		}
		// Skip the virtual partitions e.g. /proc
		if currentDiskUsage.String() == "" {
			continue
		}
		diskUsage = append(diskUsage, *currentDiskUsage)
	}
	return diskUsage, nil
}

func updateSystemMetrics() (sysMetrics sysMetrics, err error) {
	// Try to update system metrics. Do not return if we could not update some of them.
	if sysMetrics.Memory, err = getHostVirtualMemory(); err != nil {
		log.Error(err)
	}

	if sysMetrics.LoadAvarage, err = getHostLoadAvarage(); err != nil {
		log.Error(err)
	}

	// Get all partitions available on a host.
	if sysMetrics.Partitions, err = getHostDiskPartitions(true); err != nil {
		// If we could not get a list of partitions then return. Disk usage requires a list of partitions.
		return sysMetrics, err
	}

	if sysMetrics.DiskUsage, err = getHostDiskUsage(sysMetrics.Partitions); err != nil {
		return sysMetrics, err
	}

	return sysMetrics, nil
}

func logError(err error) {
	if err != nil {
		log.Error(err)
	}
}

// GetUnitsProperties return a structured units health response of UnitsHealthResponseJsonStruct type.
func GetUnitsProperties(config *Config) (healthReport UnitsHealthResponseJSONStruct, err error) {
	// update system metrics first to make sure we always return them.
	sysMetrics, err := updateSystemMetrics()
	if err != nil {
		log.Error(err)
	}
	healthReport.System = sysMetrics

	// detect DC/OS systemd units
	foundUnits, err := config.DCOSTools.GetUnitNames()
	if err != nil {
		log.Error(err)
	}
	var allUnitsProperties []healthResponseValues
	// open dbus connection
	if err = config.DCOSTools.InitializeDbusConnection(); err != nil {
		return healthReport, err
	}
	log.Debug("Opened dbus connection")

	// DCOS-5862 blacklist systemd units
	excludeUnits := []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}

	units := append(config.SystemdUnits, foundUnits...)
	for _, unit := range units {
		if isInList(unit, excludeUnits) {
			log.Debugf("Skipping blacklisted systemd unit %s", unit)
			continue
		}
		currentProperty, err := config.DCOSTools.GetUnitProperties(unit)
		if err != nil {
			log.Errorf("Could not get properties for unit: %s", unit)
			continue
		}
		allUnitsProperties = append(allUnitsProperties, normalizeProperty(unit, currentProperty, config.DCOSTools))
	}
	// after we finished querying systemd units, close dbus connection
	if err = config.DCOSTools.CloseDbusConnection(); err != nil {
		// we should probably return here, since we cannot guarantee that all units have been queried.
		return healthReport, err
	}

	// update the rest of healthReport fields
	healthReport.Array = allUnitsProperties
	healthReport.Hostname, err = config.DCOSTools.GetHostname()
	logError(err)

	healthReport.IPAddress, err = config.DCOSTools.DetectIP()
	logError(err)

	healthReport.DcosVersion = config.DCOSVersion
	healthReport.Role, err = config.DCOSTools.GetNodeRole()
	logError(err)

	healthReport.MesosID, err = config.DCOSTools.GetMesosNodeID(config.DCOSTools.GetNodeRole)
	logError(err)
	healthReport.TdtVersion = config.Version

	return healthReport, nil
}
