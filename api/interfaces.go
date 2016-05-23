package api

import "time"

// Puller interface
type Puller interface {
	// function to get a list of masters uses dns lookup of master.mesos, append to *[]Host
	LookupMaster() ([]Node, error)

	// function gets a list of agents from master.mesos:5050/slaves, append to *[]Host
	GetAgentsFromMaster() ([]Node, error)

	// functions make a GET request to a remote node, return an array of response, response status and error
	GetUnitsPropertiesViaHTTP(string) ([]byte, int, error)

	// function to wait between pulls
	WaitBetweenPulls(int)

	// Get timestamp
	GetTimestamp() time.Time
}

// DCOSHelper DC/OS specific tools interface.
type DCOSHelper interface {
	// open dbus connection
	InitializeDbusConnection() error

	// close dbus connection
	CloseDbusConnection() error

	// function to get Connection.GetUnitProperties(pname)
	// returns a maps of properties https://github.com/coreos/go-systemd/blob/master/dbus/methods.go#L176
	GetUnitProperties(string) (map[string]interface{}, error)

	// A wrapper to /opt/mesosphere/bin/detect_ip script
	// should return empty string if script fails.
	DetectIP() (string, error)

	// get system's hostname
	GetHostname() (string, error)

	// Detect node role: master/agent
	GetNodeRole() (string, error)

	// Get DC/OS systemd units on a system
	GetUnitNames() ([]string, error)

	// Get journal output
	GetJournalOutput(string) (string, error)

	// Get mesos node id, first argument is a function to determine a role.
	GetMesosNodeID(func() (string, error)) (string, error)
}

// with the nodeFinder interface we can chain finding methods
type nodeFinder interface {
	find() ([]Node, error)
}
