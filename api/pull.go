package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/dcos/3dt/config"
	"github.com/dcos/dcos-go/dcos"
)

const (
	// MasterRole DC/OS role for a master.
	MasterRole = dcos.RoleMaster

	// AgentRole DC/OS role for an agent.
	AgentRole = dcos.RoleAgent

	// AgentPublicRole DC/OS role for a public agent.
	AgentPublicRole = dcos.RoleAgentPublic
)

// find masters via dns. Used to find master nodes from agents.
type findMastersInExhibitor struct {
	url  string
	next nodeFinder

	// getFn takes url and timeout and returns a read body, HTTP status code and error.
	getFn func(string, time.Duration) ([]byte, int, error)
}

func (f *findMastersInExhibitor) findMesosMasters() (nodes []Node, err error) {
	if f.getFn == nil {
		return nodes, errors.New("could not initialize HTTP GET function. Make sure you set getFn in the constructor")
	}
	timeout := time.Duration(time.Second * 11)
	body, statusCode, err := f.getFn(f.url, timeout)
	if err != nil {
		return nodes, err
	}
	if statusCode != http.StatusOK {
		return nodes, fmt.Errorf("GET %s failed, status code: %d", f.url, statusCode)
	}

	var exhibitorNodesResponse []exhibitorNodeResponse
	if err := json.Unmarshal([]byte(body), &exhibitorNodesResponse); err != nil {
		return nodes, err
	}
	if len(exhibitorNodesResponse) == 0 {
		return nodes, errors.New("master nodes not found in exhibitor")
	}

	for _, exhibitorNodeResponse := range exhibitorNodesResponse {
		nodes = append(nodes, Node{
			Role:   MasterRole,
			IP:     exhibitorNodeResponse.Hostname,
			Leader: exhibitorNodeResponse.IsLeader,
		})
	}
	return nodes, nil
}

func (f *findMastersInExhibitor) find() (nodes []Node, err error) {
	nodes, err = f.findMesosMasters()
	if err == nil {
		logrus.Debug("Found masters in exhibitor")
		return nodes, nil
	}
	// try next provider if it is available
	if f.next != nil {
		logrus.Warning(err)
		return f.next.find()
	}
	return nodes, err
}

// NodesNotFoundError is a custom error called when nodes are not found.
type NodesNotFoundError struct {
	msg string
}

func (n NodesNotFoundError) Error() string {
	return n.msg
}

// find agents in history service
type findAgentsInHistoryService struct {
	pastTime string
	next     nodeFinder
}

func (f *findAgentsInHistoryService) getMesosAgents() (nodes []Node, err error) {
	basePath := "/var/lib/dcos/dcos-history" + f.pastTime
	files, err := ioutil.ReadDir(basePath)
	if err != nil {
		return nodes, err
	}
	nodeCount := make(map[string]int)
	for _, historyFile := range files {
		filePath := filepath.Join(basePath, historyFile.Name())
		agents, err := ioutil.ReadFile(filePath)
		if err != nil {
			logrus.Errorf("Could not read %s: %s", filePath, err)
			continue
		}

		unquotedAgents, err := strconv.Unquote(string(agents))
		if err != nil {
			logrus.Errorf("Could not unquote agents string %s: %s", string(agents), err)
			continue
		}

		var sr agentsResponse
		if err := json.Unmarshal([]byte(unquotedAgents), &sr); err != nil {
			logrus.Errorf("Could not unmarshal unquotedAgents %s: %s", unquotedAgents, err)
			continue
		}

		for _, agent := range sr.Agents {
			if _, ok := nodeCount[agent.Hostname]; ok {
				nodeCount[agent.Hostname]++
			} else {
				nodeCount[agent.Hostname] = 1
			}
		}

	}
	if len(nodeCount) == 0 {
		return nodes, NodesNotFoundError{
			msg: fmt.Sprintf("Agent nodes were not found in history service for the past %s", f.pastTime),
		}
	}

	for ip := range nodeCount {
		nodes = append(nodes, Node{
			Role: AgentRole,
			IP:   ip,
		})
	}
	return nodes, nil
}

func (f *findAgentsInHistoryService) find() (nodes []Node, err error) {
	nodes, err = f.getMesosAgents()
	if err == nil {
		logrus.Debugf("Found agents in the history service for past %s", f.pastTime)
		return nodes, nil
	}
	// try next provider if it is available
	if f.next != nil {
		logrus.Warning(err)
		return f.next.find()
	}
	return nodes, err
}

// find agents by resolving dns entry
type findNodesInDNS struct {
	forceTLS  bool
	dnsRecord string
	role      string
	next      nodeFinder

	// getFn takes url and timeout and returns a read body, HTTP status code and error.
	getFn func(string, time.Duration) ([]byte, int, error)
}

func (f *findNodesInDNS) resolveDomain() (ips []string, err error) {
	return net.LookupHost(f.dnsRecord)
}

func (f *findNodesInDNS) getMesosMasters() (nodes []Node, err error) {
	ips, err := f.resolveDomain()
	if err != nil {
		return nodes, err
	}
	if len(ips) == 0 {
		return nodes, errors.New("Could not resolve " + f.dnsRecord)
	}

	for _, ip := range ips {
		nodes = append(nodes, Node{
			Role: MasterRole,
			IP:   ip,
		})
	}
	return nodes, nil
}

func (f *findNodesInDNS) getMesosAgents() (nodes []Node, err error) {
	if f.getFn == nil {
		return nodes, errors.New("Could not initialize HTTP GET function. Make sure you set getFn in constractor")
	}
	leaderIps, err := f.resolveDomain()
	if err != nil {
		return nodes, err
	}
	if len(leaderIps) == 0 {
		return nodes, errors.New("Could not resolve " + f.dnsRecord)
	}

	url, err := useTLSScheme(fmt.Sprintf("http://%s:5050/slaves", leaderIps[0]), f.forceTLS)
	if err != nil {
		return nodes, err
	}

	timeout := time.Duration(time.Second)
	body, statusCode, err := f.getFn(url, timeout)
	if err != nil {
		return nodes, err
	}
	if statusCode != http.StatusOK {
		return nodes, fmt.Errorf("GET %s failed, status code %d", url, statusCode)
	}

	var sr agentsResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nodes, err
	}

	for _, agent := range sr.Agents {
		role := AgentRole

		// if a node has "attributes": {"public_ip": "true"} we consider it to be a public agent
		if agent.Attributes.PublicIP == "true" {
			role = AgentPublicRole
		}
		nodes = append(nodes, Node{
			Role: role,
			IP:   agent.Hostname,
		})
	}
	return nodes, nil
}

func (f *findNodesInDNS) dispatchGetNodesByRole() (nodes []Node, err error) {
	if f.role == MasterRole {
		return f.getMesosMasters()
	}
	if f.role != AgentRole {
		return nodes, fmt.Errorf("%s role is incorrect, must be %s or %s", f.role, MasterRole, AgentRole)
	}
	return f.getMesosAgents()
}

func (f *findNodesInDNS) find() (nodes []Node, err error) {
	nodes, err = f.dispatchGetNodesByRole()
	if err == nil {
		logrus.Debugf("Found %s nodes by resolving %s", f.role, f.dnsRecord)
		return nodes, err
	}
	if f.next != nil {
		logrus.Warning(err)
		return f.next.find()
	}
	return nodes, err
}

// UpdateMonitoringResponse will update the status tree.
func (mr *MonitoringResponse) UpdateMonitoringResponse(r *MonitoringResponse) {
	mr.Lock()
	defer mr.Unlock()
	mr.Nodes = r.Nodes
	mr.Units = r.Units
	mr.UpdatedTime = r.UpdatedTime
}

// GetAllUnits returns all systemd units from status tree.
func (mr *MonitoringResponse) GetAllUnits() UnitsResponseJSONStruct {
	mr.Lock()
	defer mr.Unlock()
	return UnitsResponseJSONStruct{
		Array: func() []UnitResponseFieldsStruct {
			var r []UnitResponseFieldsStruct
			for _, unit := range mr.Units {
				r = append(r, UnitResponseFieldsStruct{
					unit.UnitName,
					unit.PrettyName,
					unit.Health,
					unit.Title,
				})
			}
			return r
		}(),
	}
}

// GetUnit gets a specific Unit from a status tree.
func (mr *MonitoringResponse) GetUnit(unitName string) (UnitResponseFieldsStruct, error) {
	mr.Lock()
	defer mr.Unlock()
	fmt.Printf("Trying to look for %s\n", unitName)
	if _, ok := mr.Units[unitName]; !ok {
		return UnitResponseFieldsStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}

	return UnitResponseFieldsStruct{
		mr.Units[unitName].UnitName,
		mr.Units[unitName].PrettyName,
		mr.Units[unitName].Health,
		mr.Units[unitName].Title,
	}, nil

}

// GetNodesForUnit get all hosts for a specific Unit available in status tree.
func (mr *MonitoringResponse) GetNodesForUnit(unitName string) (NodesResponseJSONStruct, error) {
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Units[unitName]; !ok {
		return NodesResponseJSONStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}
	return NodesResponseJSONStruct{
		Array: func() []*NodeResponseFieldsStruct {
			var r []*NodeResponseFieldsStruct
			for _, node := range mr.Units[unitName].Nodes {
				r = append(r, &NodeResponseFieldsStruct{
					node.IP,
					node.Health,
					node.Role,
				})
			}
			return r
		}(),
	}, nil
}

// GetSpecificNodeForUnit gets a specific node for a given Unit from a status tree.
func (mr *MonitoringResponse) GetSpecificNodeForUnit(unitName string, nodeIP string) (NodeResponseFieldsWithErrorStruct, error) {
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Units[unitName]; !ok {
		return NodeResponseFieldsWithErrorStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}

	for _, node := range mr.Units[unitName].Nodes {
		if node.IP == nodeIP {
			helpField := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", node.MesosID)
			return NodeResponseFieldsWithErrorStruct{
				node.IP,
				node.Health,
				node.Role,
				node.Output[unitName],
				helpField,
			}, nil
		}
	}
	return NodeResponseFieldsWithErrorStruct{}, fmt.Errorf("Node %s not found", nodeIP)
}

// GetNodes gets all available nodes in status tree.
func (mr *MonitoringResponse) GetNodes() NodesResponseJSONStruct {
	mr.Lock()
	defer mr.Unlock()
	return NodesResponseJSONStruct{
		Array: func() []*NodeResponseFieldsStruct {
			var nodes []*NodeResponseFieldsStruct
			for _, node := range mr.Nodes {
				nodes = append(nodes, &NodeResponseFieldsStruct{
					node.IP,
					node.Health,
					node.Role,
				})
			}
			return nodes
		}(),
	}
}

// GetMasterAgentNodes returns a list of master and agent nodes available in status tree.
func (mr *MonitoringResponse) GetMasterAgentNodes() ([]Node, []Node, error) {
	mr.Lock()
	defer mr.Unlock()

	var masterNodes, agentNodes []Node
	for _, node := range mr.Nodes {
		if node.Role == MasterRole {
			masterNodes = append(masterNodes, node)
			continue
		}

		if node.Role == AgentRole || node.Role == AgentPublicRole {
			agentNodes = append(agentNodes, node)
		}
	}

	if len(masterNodes) == 0 && len(agentNodes) == 0 {
		return masterNodes, agentNodes, errors.New("No nodes found in memory, perhaps 3dt was started without -pull flag")
	}

	return masterNodes, agentNodes, nil
}

// GetNodeByID returns a node by IP address from a status tree.
func (mr *MonitoringResponse) GetNodeByID(nodeIP string) (NodeResponseFieldsStruct, error) {
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return NodeResponseFieldsStruct{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	return NodeResponseFieldsStruct{
		mr.Nodes[nodeIP].IP,
		mr.Nodes[nodeIP].Health,
		mr.Nodes[nodeIP].Role,
	}, nil
}

// GetNodeUnitsID returns a Unit status for a given node from status tree.
func (mr *MonitoringResponse) GetNodeUnitsID(nodeIP string) (UnitsResponseJSONStruct, error) {
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return UnitsResponseJSONStruct{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	return UnitsResponseJSONStruct{
		Array: func(nodeIp string) []UnitResponseFieldsStruct {
			var units []UnitResponseFieldsStruct
			for _, unit := range mr.Nodes[nodeIp].Units {
				units = append(units, UnitResponseFieldsStruct{
					unit.UnitName,
					unit.PrettyName,
					unit.Health,
					unit.Title,
				})
			}
			return units
		}(nodeIP),
	}, nil
}

// GetNodeUnitByNodeIDUnitID returns a Unit status by node IP address and Unit ID.
func (mr *MonitoringResponse) GetNodeUnitByNodeIDUnitID(nodeIP string, unitID string) (HealthResponseValues, error) {
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return HealthResponseValues{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	for _, unit := range mr.Nodes[nodeIP].Units {
		if unit.UnitName == unitID {
			helpField := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", mr.Nodes[nodeIP].MesosID)
			return HealthResponseValues{
				UnitID:     unit.UnitName,
				UnitHealth: unit.Health,
				UnitOutput: mr.Nodes[nodeIP].Output[unit.UnitName],
				UnitTitle:  unit.Title,
				Help:       helpField,
				PrettyName: unit.PrettyName,
			}, nil
		}
	}
	return HealthResponseValues{}, fmt.Errorf("Unit %s not found", unitID)
}

// GetLastUpdatedTime returns timestamp of latest updated monitoring response.
func (mr *MonitoringResponse) GetLastUpdatedTime() string {
	mr.Lock()
	defer mr.Unlock()
	if mr.UpdatedTime.IsZero() {
		return ""
	}
	return mr.UpdatedTime.Format(time.ANSIC)
}

// StartPullWithInterval will start to pull a DC/OS cluster health status
func StartPullWithInterval(dt *Dt) {
	// Start infinite loop
	for {
		runPull(dt)
	inner:
		select {
		case <-dt.RunPullerChan:
			logrus.Debug("Update cluster health request recevied")
			runPull(dt)
			dt.RunPullerDoneChan <- true
			goto inner

		case <-time.After(time.Duration(dt.Cfg.FlagPullInterval) * time.Second):
			logrus.Debugf("Update cluster health after %d interval", dt.Cfg.FlagPullInterval)
		}

	}
}

func runPull(dt *Dt) {
	clusterNodes, err := dt.DtDCOSTools.GetMasterNodes()
	if err != nil {
		logrus.Errorf("Could not get master nodes: %s", err)
	}

	agentNodes, err := dt.DtDCOSTools.GetAgentNodes()
	if err != nil {
		logrus.Errorf("Could not get agent nodes: %s", err)
	}

	clusterNodes = append(clusterNodes, agentNodes...)

	// If not nodes found we should wait for a timeout between trying the next pull.
	if len(clusterNodes) == 0 {
		logrus.Error("Could not find master or agent nodes")
		return
	}

	respChan := make(chan *httpResponse, len(clusterNodes))

	// Pull data from each host
	var wg sync.WaitGroup
	for _, node := range clusterNodes {
		wg.Add(1)
		go pullHostStatus(node, respChan, dt, &wg)
	}
	wg.Wait()

	// update collected units/nodes health statuses
	updateHealthStatus(respChan, dt)
}

// function builds a map of all unique units with status
func updateHealthStatus(responses <-chan *httpResponse, dt *Dt) {
	var (
		units = make(map[string]Unit)
		nodes = make(map[string]Node)
	)

	for {
		select {
		case response := <-responses:
			node := response.Node
			node.Units = response.Units
			nodes[response.Node.IP] = node

			for _, currentUnit := range response.Units {
				u, ok := units[currentUnit.UnitName]
				if ok {
					u.Nodes = append(u.Nodes, currentUnit.Nodes...)
					if currentUnit.Health > u.Health {
						u.Health = currentUnit.Health
					}
					units[currentUnit.UnitName] = u
				} else {
					units[currentUnit.UnitName] = currentUnit
				}
			}
		default:
			dt.MR.UpdateMonitoringResponse(&MonitoringResponse{
				Nodes:       nodes,
				Units:       units,
				UpdatedTime: time.Now(),
			})
			return
		}
	}
}

func pullHostStatus(host Node, respChan chan<- *httpResponse, dt *Dt, wg *sync.WaitGroup) {
	defer wg.Done()
	var response httpResponse
	port, err := getPullPortByRole(dt.Cfg, host.Role)
	if err != nil {
		logrus.Errorf("Could not get a port by role %s: %s", host.Role, err)
		response.Status = http.StatusServiceUnavailable
		host.Health = 3
		response.Node = host
		respChan <- &response
		return
	}

	baseURL := fmt.Sprintf("http://%s:%d%s", host.IP, port, BaseRoute)

	// UnitsRoute available in router.go
	url, err := useTLSScheme(baseURL, dt.Cfg.FlagForceTLS)
	if err != nil {
		logrus.Errorf("Could not read useTLSScheme: %s", err)
		response.Status = http.StatusServiceUnavailable
		host.Health = 3
		response.Node = host
		respChan <- &response
		return
	}

	// Make a request to get node units status
	// use fake interface implementation for tests
	timeout := time.Duration(dt.Cfg.FlagPullTimeoutSec) * time.Second
	body, statusCode, err := dt.DtDCOSTools.Get(url, timeout)
	if err != nil {
		logrus.Errorf("Could not HTTP GET %s: %s", url, err)
		response.Status = statusCode
		host.Health = 3 // 3 stands for unknown
		respChan <- &response
		response.Node = host
		return
	}

	// Response should be strictly mapped to jsonBodyStruct, otherwise skip it
	var jsonBody UnitsHealthResponseJSONStruct
	if err := json.Unmarshal(body, &jsonBody); err != nil {
		logrus.Errorf("Could not deserialize json reponse from %s, url %s: %s", host.IP, url, err)
		response.Status = statusCode
		host.Health = 3 // 3 stands for unknown
		respChan <- &response
		response.Node = host
		return
	}
	response.Status = statusCode

	// Update Response and send it back to respChan
	host.Host = jsonBody.Hostname

	// update mesos node id
	host.MesosID = jsonBody.MesosID

	host.Output = make(map[string]string)

	// if at least one Unit is not healthy, the host should be set unhealthy
	for _, propertiesMap := range jsonBody.Array {
		if propertiesMap.UnitHealth > host.Health {
			host.Health = propertiesMap.UnitHealth
			break
		}
	}

	for _, propertiesMap := range jsonBody.Array {
		// update error message per host per Unit
		host.Output[propertiesMap.UnitID] = propertiesMap.UnitOutput
		response.Units = append(response.Units, Unit{
			propertiesMap.UnitID,
			[]Node{host},
			propertiesMap.UnitHealth,
			propertiesMap.UnitTitle,
			dt.DtDCOSTools.GetTimestamp(),
			propertiesMap.PrettyName,
		})
	}
	response.Node = host
	respChan <- &response

}

func getPullPortByRole(cfg *config.Config, role string) (int, error) {
	var port int
	if role != MasterRole && role != AgentRole && role != AgentPublicRole {
		return port, fmt.Errorf("Incorrect role %s, must be: %s, %s or %s", role, MasterRole, AgentRole, AgentPublicRole)
	}
	port = cfg.FlagAgentPort
	if role == MasterRole {
		port = cfg.FlagMasterPort
	}
	return port, nil
}
