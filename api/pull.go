package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
	"net"
	"path/filepath"
	"strconv"
)

const (
	// MasterRole DC/OS role for a master.
	MasterRole = "master"

	// AgentRole DC/OS role for an agent.
	AgentRole = "agent"

	// AgentPublicRole DC/OS role for a public agent.
	AgentPublicRole = "agent_public"
)

// globalMonitoringResponse a global variable updated by a puller every 60 seconds.
var globalMonitoringResponse monitoringResponse

// find masters via dns. Used to find master nodes from agents.
type findMastersInExhibitor struct {
	url  string
	next nodeFinder

	// getFn takes url and timeout and returns a read body, HTTP status code and error.
	getFn func(string, time.Duration) ([]byte, int, error)
}

func (f *findMastersInExhibitor) findMesosMasters() (nodes []Node, err error) {
	if f.getFn == nil {
		return nodes, errors.New("Could not initialize HTTP GET function. Make sure you set getFn in constractor")
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
		log.Debug("Found masters in exhibitor")
		return nodes, nil
	}
	// try next provider if it is available
	if f.next != nil {
		log.Warning(err)
		return f.next.find()
	}
	return nodes, err
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
			log.Errorf("Could not read %s: %s", filePath, err)
			continue
		}

		unquotedAgents, err := strconv.Unquote(string(agents))
		if err != nil {
			log.Errorf("Could not unquote agents string %s: %s", string(agents), err)
			continue
		}

		var sr agentsResponse
		if err := json.Unmarshal([]byte(unquotedAgents), &sr); err != nil {
			log.Errorf("Could not unmarshal unquotedAgents %s: %s", unquotedAgents, err)
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
		return nodes, errors.New("Agent nodes were not found in history service for the past hour")
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
		log.Debugf("Found agents in the history service for past %s", f.pastTime)
		return nodes, nil
	}
	// try next provider if it is available
	if f.next != nil {
		log.Warning(err)
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
		log.Debugf("Found %s nodes by resolving %s", f.role, f.dnsRecord)
		return nodes, err
	}
	if f.next != nil {
		log.Warning(err)
		return f.next.find()
	}
	return nodes, err
}

func (mr *monitoringResponse) updateMonitoringResponse(r monitoringResponse) {
	mr.Lock()
	defer mr.Unlock()
	mr.Nodes = r.Nodes
	mr.Units = r.Units
}

// Get all units available in globalMonitoringResponse
func (mr *monitoringResponse) GetAllUnits() unitsResponseJSONStruct {
	mr.RLock()
	defer mr.RUnlock()
	return unitsResponseJSONStruct{
		Array: func() []unitResponseFieldsStruct {
			var r []unitResponseFieldsStruct
			for _, unit := range mr.Units {
				r = append(r, unitResponseFieldsStruct{
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

// Get a specific unit available in globalMonitoringResponse
func (mr *monitoringResponse) GetUnit(unitName string) (unitResponseFieldsStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return unitResponseFieldsStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}

	return unitResponseFieldsStruct{
		mr.Units[unitName].UnitName,
		mr.Units[unitName].PrettyName,
		mr.Units[unitName].Health,
		mr.Units[unitName].Title,
	}, nil

}

// Get all hosts for a specific unit available in GlobalMonitoringResponse
func (mr *monitoringResponse) GetNodesForUnit(unitName string) (nodesResponseJSONStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return nodesResponseJSONStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}
	return nodesResponseJSONStruct{
		Array: func() []*nodeResponseFieldsStruct {
			var r []*nodeResponseFieldsStruct
			for _, node := range mr.Units[unitName].Nodes {
				r = append(r, &nodeResponseFieldsStruct{
					node.IP,
					node.Health,
					node.Role,
				})
			}
			return r
		}(),
	}, nil
}

func (mr *monitoringResponse) GetSpecificNodeForUnit(unitName string, nodeIP string) (nodeResponseFieldsWithErrorStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return nodeResponseFieldsWithErrorStruct{}, fmt.Errorf("Unit %s not found", unitName)
	}

	for _, node := range mr.Units[unitName].Nodes {
		if node.IP == nodeIP {
			helpField := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", node.MesosID)
			return nodeResponseFieldsWithErrorStruct{
				node.IP,
				node.Health,
				node.Role,
				node.Output[unitName],
				helpField,
			}, nil
		}
	}
	return nodeResponseFieldsWithErrorStruct{}, fmt.Errorf("Node %s not found")
}

func (mr *monitoringResponse) GetNodes() nodesResponseJSONStruct {
	mr.RLock()
	defer mr.RUnlock()
	return nodesResponseJSONStruct{
		Array: func() []*nodeResponseFieldsStruct {
			var nodes []*nodeResponseFieldsStruct
			for _, node := range mr.Nodes {
				nodes = append(nodes, &nodeResponseFieldsStruct{
					node.IP,
					node.Health,
					node.Role,
				})
			}
			return nodes
		}(),
	}
}

func (mr *monitoringResponse) getMasterAgentNodes() ([]Node, []Node, error) {
	mr.RLock()
	defer mr.RUnlock()

	var masterNodes, agentNodes []Node
	for _, node := range mr.Nodes {
		if node.Role == MasterRole {
			masterNodes = append(masterNodes, *node)
			continue
		}

		if node.Role == AgentRole || node.Role == AgentPublicRole {
			agentNodes = append(agentNodes, *node)
		}
	}

	if len(masterNodes) == 0 && len(agentNodes) == 0 {
		return masterNodes, agentNodes, errors.New("No nodes found in memory, perhaps 3dt was started without -pull flag")
	}

	return masterNodes, agentNodes, nil
}

func (mr *monitoringResponse) GetNodeByID(nodeIP string) (nodeResponseFieldsStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return nodeResponseFieldsStruct{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	return nodeResponseFieldsStruct{
		mr.Nodes[nodeIP].IP,
		mr.Nodes[nodeIP].Health,
		mr.Nodes[nodeIP].Role,
	}, nil
}

func (mr *monitoringResponse) GetNodeUnitsID(nodeIP string) (unitsResponseJSONStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return unitsResponseJSONStruct{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	return unitsResponseJSONStruct{
		Array: func(nodeIp string) []unitResponseFieldsStruct {
			var units []unitResponseFieldsStruct
			for _, unit := range mr.Nodes[nodeIp].Units {
				units = append(units, unitResponseFieldsStruct{
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

func (mr *monitoringResponse) GetNodeUnitByNodeIDUnitID(nodeIP string, unitID string) (healthResponseValues, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIP]; !ok {
		return healthResponseValues{}, fmt.Errorf("Node %s not found", nodeIP)
	}
	for _, unit := range mr.Nodes[nodeIP].Units {
		if unit.UnitName == unitID {
			helpField := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", mr.Nodes[nodeIP].MesosID)
			return healthResponseValues{
				UnitID:     unit.UnitName,
				UnitHealth: unit.Health,
				UnitOutput: mr.Nodes[nodeIP].Output[unit.UnitName],
				UnitTitle:  unit.Title,
				Help:       helpField,
				PrettyName: unit.PrettyName,
			}, nil
		}
	}
	return healthResponseValues{}, fmt.Errorf("Unit %s not found", unitID)
}

// StartPullWithInterval will start to pull a DC/OS cluster health status
func StartPullWithInterval(dt Dt, ready chan struct{}) {
	select {
	case <-ready:
		log.Infof("Start pulling with interval %d", dt.Cfg.FlagPullInterval)
	case <-time.After(time.Second * 10):
		log.Error("Not ready to pull from localhost after 10 seconds")
	}

	// run puller for the first time
	runPull(dt, false)

	// Start infinite loop
	for {
		select {
		case <-dt.RunPullerChan:
			log.Debug("Update cluster health request recevied")
			runPull(dt, true)

			// signal back that runPull has been executed and data is ready
			dt.RunPullerDoneChan <- true
			break
		case <-time.After(time.Duration(dt.Cfg.FlagPullInterval) * time.Second):
			// run puller after a timeout
			runPull(dt, false)
			break
		}
	}
}

func runPull(dt Dt, noCache bool) {
	var clusterHosts []Node
	masterNodes, err := dt.DtDCOSTools.GetMasterNodes()
	if err != nil {
		log.Errorf("Could not get master nodes: %s", err)
	}

	agentNodes, err := dt.DtDCOSTools.GetAgentNodes()
	if err != nil {
		log.Errorf("Could not get agent nodes: %s", err)
	}

	clusterHosts = append(clusterHosts, masterNodes...)
	clusterHosts = append(clusterHosts, agentNodes...)

	// If not nodes found we should wait for a timeout between trying the next pull.
	if len(clusterHosts) == 0 {
		log.Error("Could not find master or agent nodes")
		return
	}

	respChan := make(chan *httpResponse, len(clusterHosts))
	hostsChan := make(chan Node, len(clusterHosts))
	loadJobs(hostsChan, clusterHosts)

	// Pull data from each host
	for i := 0; i < len(clusterHosts); i++ {
		go pullHostStatus(hostsChan, respChan, dt, noCache)
	}

	// blocking here got get all responses from hosts
	clusterHTTPResponses := collectResponses(respChan, len(clusterHosts))

	// update collected units/nodes health statuses
	updateHealthStatus(clusterHTTPResponses)
}

// function builds a map of all unique units with status
func updateHealthStatus(responses []*httpResponse) {
	units := make(map[string]*unit)
	nodes := make(map[string]*Node)

	for httpResponseIndex, httpResponse := range responses {
		if httpResponse.Status != http.StatusOK {
			nodes[httpResponse.Node.IP] = &httpResponse.Node
			log.Errorf("Status code: %d", httpResponse.Status)
			continue
		}
		if _, ok := nodes[httpResponse.Node.IP]; !ok {
			// copy a node, to avoid circular loop
			newNode := responses[httpResponseIndex].Node
			nodes[httpResponse.Node.IP] = &newNode
		}
		for unitResponseIndex, unitResponse := range httpResponse.Units {
			// we don't want to have circular explosion here, so make a brand new Unit{} with all fields
			// but []*Node{}
			nodes[httpResponse.Node.IP].Units = append(nodes[httpResponse.Node.IP].Units, unitResponse)
			if _, ok := units[unitResponse.UnitName]; ok {
				// Append nodes
				for _, node := range unitResponse.Nodes {
					units[unitResponse.UnitName].Nodes = append(units[unitResponse.UnitName].Nodes, node)
				}
				if unitResponse.Health > units[unitResponse.UnitName].Health {
					units[unitResponse.UnitName].Health = unitResponse.Health
				}
			} else {
				// make sure our reference does not go away
				units[unitResponse.UnitName] = &responses[httpResponseIndex].Units[unitResponseIndex]
			}
		}
	}
	log.Debugf("Number of nodes: %d, len of responses: %d", len(nodes), len(responses))
	globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{
		Units: units,
		Nodes: nodes,
	})
}

func collectResponses(respChan <-chan *httpResponse, totalHosts int) (responses []*httpResponse) {
	for {
		if totalHosts == 0 {
			log.Debug("Nothing to process")
			return responses
		}
		select {
		case r := <-respChan:
			// Add the response to the return array
			responses = append(responses, r)
			log.Debug("Responses ", len(responses), " total hosts ", totalHosts)
			if len(responses) == totalHosts {
				return responses
			}
		// If there is nothing on the channel, timeout for 1s and print our total processed
		// responses so far.
		case <-time.After(time.Second):
			log.Debugf("Processed responses: %d", len(responses))
			return responses
		}
	}
}

func pullHostStatus(hosts <-chan Node, respChan chan<- *httpResponse, dt Dt, noCache bool) {
	for host := range hosts {
		var response httpResponse

		port, err := getPullPortByRole(dt.Cfg, host.Role)
		if err != nil {
			log.Errorf("Could not get a port by role %s: %s", host.Role, err)
			response.Status = http.StatusServiceUnavailable
			host.Health = 3
			response.Node = host
			respChan <- &response
			continue
		}

		baseURL := fmt.Sprintf("http://%s:%d%s", host.IP, port, BaseRoute)
		if noCache {
			baseURL += "?cache=0"
		}

		// UnitsRoute available in router.go
		url, err := useTLSScheme(baseURL, dt.Cfg.FlagForceTLS)
		if err != nil {
			log.Errorf("Could not read useTLSScheme: %s", err)
			response.Status = http.StatusServiceUnavailable
			host.Health = 3
			response.Node = host
			respChan <- &response
			continue
		}

		// Make a request to get node units status
		// use fake interface implementation for tests
		timeout := time.Duration(3 * time.Second)
		body, statusCode, err := dt.DtDCOSTools.Get(url, timeout)
		if err != nil {
			log.Errorf("Could not HTTP GET %s: %s", url, err)
			response.Status = statusCode
			host.Health = 3 // 3 stands for unknown
			respChan <- &response
			response.Node = host
			continue
		}

		// Response should be strictly mapped to jsonBodyStruct, otherwise skip it
		var jsonBody UnitsHealthResponseJSONStruct
		if err := json.Unmarshal(body, &jsonBody); err != nil {
			log.Errorf("Coult not deserialize json reponse from %s, url %s: %s", host.IP, url, err)
			response.Status = statusCode
			host.Health = 3 // 3 stands for unknown
			respChan <- &response
			response.Node = host
			continue
		}
		response.Status = statusCode

		// Update Response and send it back to respChan
		host.Host = jsonBody.Hostname

		// update mesos node id
		host.MesosID = jsonBody.MesosID

		host.Output = make(map[string]string)

		// if at least one unit is not healthy, the host should be set unhealthy
		for _, propertiesMap := range jsonBody.Array {
			if propertiesMap.UnitHealth > host.Health {
				host.Health = propertiesMap.UnitHealth
				break
			}
		}

		for _, propertiesMap := range jsonBody.Array {
			// update error message per host per unit
			host.Output[propertiesMap.UnitID] = propertiesMap.UnitOutput
			response.Units = append(response.Units, unit{
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
}

func loadJobs(jobChan chan Node, hosts []Node) {
	for _, u := range hosts {
		log.Debugf("Adding %s to jobs processing channel", u.IP)
		jobChan <- u
	}
	// we should close the channel, since we will recreate a list every 60 sec
	close(jobChan)
}

func getPullPortByRole(config *Config, role string) (int, error) {
	var port int
	if role != MasterRole && role != AgentRole && role != AgentPublicRole {
		return port, fmt.Errorf("Incorrect role %s, must be: %s, %s or %s", role, MasterRole, AgentRole, AgentPublicRole)
	}
	port = config.FlagAgentPort
	if role == MasterRole {
		port = config.FlagMasterPort
	}
	return port, nil
}
