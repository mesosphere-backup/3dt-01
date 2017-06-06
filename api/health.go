package api

import (
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/config"
	"github.com/shirou/gopsutil/disk"
	"github.com/shirou/gopsutil/load"
	"github.com/shirou/gopsutil/mem"
	"os"
)

// SystemdUnits used to make GetUnitsProperties thread safe.
type SystemdUnits struct {
	sync.Mutex
}

// GetUnits returns a list of found unit properties.
func (s *SystemdUnits) GetUnits(tools DCOSHelper) (allUnitsProperties []HealthResponseValues, err error) {
	if err = tools.InitializeDBUSConnection(); err != nil {
		return nil, err
	}
	defer tools.CloseDBUSConnection()

	// detect DC/OS systemd units
	foundUnits, err := tools.GetUnitNames()
	if err != nil {
		return nil, err
	}

	// DCOS-5862 blacklist systemd units
	excludeUnits := []string{"dcos-setup.service", "dcos-link-env.service", "dcos-download.service"}
	for _, unit := range foundUnits {
		if isInList(unit, excludeUnits) {
			logrus.Debugf("Skipping blacklisted systemd Unit %s", unit)
			continue
		}
		currentProperty, err := tools.GetUnitProperties(unit)
		if err != nil {
			logrus.Errorf("Could not get properties for Unit: %s", unit)
			continue
		}
		normalizedProperty, err := normalizeProperty(currentProperty, tools)
		if err != nil {
			logrus.Errorf("Could not normalize property for Unit %s: %s", unit, err)
			continue
		}
		allUnitsProperties = append(allUnitsProperties, normalizedProperty)
	}
	return allUnitsProperties, nil
}

// GetUnitsProperties return a structured units health response of UnitsHealthResponseJsonStruct type.
func (s *SystemdUnits) GetUnitsProperties(cfg *config.Config, tools DCOSHelper) (healthReport UnitsHealthResponseJSONStruct, err error) {
	s.Lock()
	defer s.Unlock()

	// update system metrics first to make sure we always return them.
	sysMetrics, err := updateSystemMetrics()
	if err != nil {
		logrus.Errorf("Could not update system metrics: %s", err)
	}
	healthReport.System = sysMetrics
	healthReport.TdtVersion = config.Version
	healthReport.Hostname, err = tools.GetHostname()
	if err != nil {
		logrus.Errorf("Could not get a hostname: %s", err)
	}

	// update the rest of healthReport fields
	healthReport.Array, err = s.GetUnits(tools)
	if err != nil {
		logrus.Errorf("Unable to get a list of systemd units: %s", err)
	}

	healthReport.IPAddress, err = tools.DetectIP()
	if err != nil {
		logrus.Errorf("Could not detect IP: %s", err)
	}

	healthReport.DcosVersion = os.Getenv("DCOS_VERSION")
	healthReport.Role, err = tools.GetNodeRole()
	if err != nil {
		logrus.Errorf("Could not get node role: %s", err)
	}

	healthReport.MesosID, err = tools.GetMesosNodeID()
	if err != nil {
		logrus.Errorf("Could not get mesos node id: %s", err)
	}

	return healthReport, nil
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
			logrus.Debugf("Could not get a disk usage [%s]: %s", diskProps.Mountpoint, err)
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
		logrus.Errorf("Could not get a host virtual memory: %s", err)
	}

	if sysMetrics.LoadAvarage, err = getHostLoadAvarage(); err != nil {
		logrus.Errorf("Could not get a host load average : %s", err)
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
