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
func StartUpdateHealthReport(dt Dt, readyChan chan struct{}, runOnce bool) {
	updateHealth := func() error {
		healthReport, err := GetUnitsProperties(dt)
		if err != nil {
			return err
		}
		unitsHealthReport.UpdateHealthReport(healthReport)
		return nil
	}

	// update health for the first time
	if err := updateHealth(); err == nil {
		close(readyChan)
	}

	if runOnce {
		return
	}

	for {
		select {
		case <- dt.UpdateHealthChan:
			log.Debug("Received an update health status signal")
			if err := updateHealth(); err != nil {
				log.Errorf("Could not update health status: %s", err)
			}
			dt.UpdateHealthDoneChan <- true
			break
		case <- time.After(time.Duration(dt.Cfg.FlagUpdateHealthReportInterval) * time.Second):
			log.Debug("Timeout reached, updating health status")
			if err := updateHealth(); err != nil {
				log.Errorf("Could not update health status: %s", err)
			}
			break
		}
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
			log.Errorf("Could not get a disk usage: %s", err)
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
		log.Errorf("Could not get a host virtual memory: %s", err)
	}

	if sysMetrics.LoadAvarage, err = getHostLoadAvarage(); err != nil {
		log.Error("Could not get a host load avarage: %s", err)
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

// GetUnitsProperties return a structured units health response of UnitsHealthResponseJsonStruct type.
func GetUnitsProperties(dt Dt) (healthReport UnitsHealthResponseJSONStruct, err error) {
	// update system metrics first to make sure we always return them.
	sysMetrics, err := updateSystemMetrics()
	if err != nil {
		log.Errorf("Could not update system metrics: %s", err)
	}
	healthReport.System = sysMetrics
	healthReport.TdtVersion = dt.Cfg.Version
	healthReport.Hostname, err = dt.DtDCOSTools.GetHostname()
	if err != nil {
		log.Errorf("Could not get a hostname: %s", err)
	}

	// detect DC/OS systemd units
	foundUnits, err := dt.DtDCOSTools.GetUnitNames()
	if err != nil {
		log.Errorf("Could not get unit names: %s", err)
	}

	var allUnitsProperties []healthResponseValues
	// open dbus connection
	if err = dt.DtDCOSTools.InitializeDBUSConnection(); err != nil {
		return healthReport, err
	}
	log.Debug("Opened dbus connection")

	// DCOS-5862 blacklist systemd units
	excludeUnits := []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}

	units := append(dt.Cfg.SystemdUnits, foundUnits...)
	for _, unit := range units {
		if isInList(unit, excludeUnits) {
			log.Debugf("Skipping blacklisted systemd unit %s", unit)
			continue
		}
		currentProperty, err := dt.DtDCOSTools.GetUnitProperties(unit)
		if err != nil {
			log.Errorf("Could not get properties for unit: %s", unit)
			continue
		}
		normalizedProperty, err := normalizeProperty(currentProperty, dt)
		if err != nil {
			log.Errorf("Could not normalize property for unit %s: %s", unit, err)
			continue
		}
		allUnitsProperties = append(allUnitsProperties, normalizedProperty)
	}
	// after we finished querying systemd units, close dbus connection
	if err = dt.DtDCOSTools.CloseDBUSConnection(); err != nil {
		// we should probably return here, since we cannot guarantee that all units have been queried.
		return healthReport, err
	}

	// update the rest of healthReport fields
	healthReport.Array = allUnitsProperties

	healthReport.IPAddress, err = dt.DtDCOSTools.DetectIP()
	if err != nil {
		log.Errorf("Could not detect IP: %s", err)
	}

	healthReport.DcosVersion = dt.Cfg.DCOSVersion
	healthReport.Role, err = dt.DtDCOSTools.GetNodeRole()
	if err != nil {
		log.Errorf("Could not get node role: %s", err)
	}

	healthReport.MesosID, err = dt.DtDCOSTools.GetMesosNodeID()
	if err != nil {
		log.Errorf("Could not get mesos node id: %s", err)
	}

	return healthReport, nil
}
