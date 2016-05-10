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
	"strconv"
)

// globalMonitoringResponse a global variable updated by a puller every 60 seconds.
var globalMonitoringResponse monitoringResponse

// DcosPuller implements Puller interface.
type DcosPuller struct{}

// GetTimestamp return time.Now()
func (pt *DcosPuller) GetTimestamp() time.Time {
	return time.Now()
}

// GetUnitsPropertiesViaHTTP make a GET request.
func (pt *DcosPuller) GetUnitsPropertiesViaHTTP(url string) ([]byte, int, error) {
	var body []byte

	// a timeout of 1 seconds should be good enough
	client := http.Client{
		Timeout: time.Duration(time.Second),
	}

	resp, err := client.Get(url)
	if err != nil {
		return body, 500, err
	}

	// http://devs.cloudimmunity.com/gotchas-and-common-mistakes-in-go-golang/index.html#close_http_resp_body
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

// LookupMaster looks for DC/OS master ip.
func (pt *DcosPuller) LookupMaster() (nodesResponse []Node, err error) {
	url := "http://127.0.0.1:8181/exhibitor/v1/cluster/status"
	client := http.Client{Timeout: time.Duration(time.Second)}
	resp, err := client.Get(url)
	if err != nil {
		log.Errorf("Could not get a list of masters, url %s", url)
		return nodesResponse, err
	}
	defer resp.Body.Close()
	nodesResponseString, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Unable to read the response from %s", url)
		return nodesResponse, err
	}

	var exhibitorNodesResponse []exhibitorNodeResponse
	if err := json.Unmarshal([]byte(nodesResponseString), &exhibitorNodesResponse); err != nil {
		log.Error("Could not deserialize nodes response")
		return nodesResponse, err
	}
	log.Debugf("Nodes response from exhibitor: %+v", exhibitorNodesResponse)

	for _, exhibitorNodeResponse := range exhibitorNodesResponse {
		var node Node
		node.Role = "master"
		node.IP = exhibitorNodeResponse.Hostname
		node.Leader = exhibitorNodeResponse.IsLeader
		nodesResponse = append(nodesResponse, node)
	}
	return nodesResponse, nil
}

// agentResponder interface implementation to get a list of agents from a leader master (dns based) or from
// history service.
type dnsResponder struct {
	defaultMasterAddress string
}

type historyServiceResponder struct {
	defaultPastTime string
}

func (hs *historyServiceResponder) getAgentSource() (jsonFiles []string, err error) {
	basePath := "/var/lib/mesosphere/dcos/history-service" + hs.defaultPastTime
	files, err := ioutil.ReadDir(basePath)
	if err != nil {
		return jsonFiles, err
	}
	for _, file := range files {
		jsonFiles = append(jsonFiles, basePath+file.Name())
	}
	return jsonFiles, nil
}

func (hs *historyServiceResponder) getMesosAgents(jsonPath []string) (nodes []Node, err error) {
	nodeCount := make(map[string]int)

	for _, historyFile := range jsonPath {
		agents, err := ioutil.ReadFile(historyFile)
		if err != nil {
			log.Error(err)
			continue
		}

		unquotedAgents, err := strconv.Unquote(string(agents))
		if err != nil {
			log.Error(err)
			continue
		}

		var sr agentsResponse
		if err := json.Unmarshal([]byte(unquotedAgents), &sr); err != nil {
			log.Error(err)
			continue
		}

		// get all nodes for the past hour
		for _, agent := range sr.Agents {
			if _, ok := nodeCount[agent.Hostname]; ok {
				nodeCount[agent.Hostname]++
			} else {
				nodeCount[agent.Hostname] = 1
			}
		}

	}
	if len(nodeCount) == 0 {
		return nodes, errors.New("Agent nodes were not found in hisotry service for the past hour")
	}
	for ip := range nodeCount {
		var node Node
		node.Role = "agent"
		node.IP = ip
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (dr *dnsResponder) getAgentSource() (leaderIps []string, err error) {
	leaderIps, err = net.LookupHost(dr.defaultMasterAddress)
	if err != nil {
		return leaderIps, err
	}
	log.Debugf("Resolving leader.mesos ip: %s", leaderIps)
	if len(leaderIps) > 1 {
		log.Warningf("leader.mesos returned more then 1 IP! %s", leaderIps)
	}
	return leaderIps, nil
}

func (dr *dnsResponder) getMesosAgents(leaderIP []string) (nodes []Node, err error) {
	agentRequest := fmt.Sprintf("http://%s:5050/slaves", leaderIP)
	timeout := time.Duration(time.Second)
	client := http.Client{Timeout: timeout}
	getAgents, err := client.Get(agentRequest)
	if err != nil {
		return nodes, err
	}
	defer getAgents.Body.Close()
	agents, err := ioutil.ReadAll(getAgents.Body)
	if err != nil {
		return nodes, err
	}

	var sr agentsResponse
	if err := json.Unmarshal([]byte(agents), &sr); err != nil {
		return nodes, err
	}

	for _, agent := range sr.Agents {
		var node Node
		node.Role = "agent"
		node.IP = agent.Hostname
		nodes = append(nodes, node)
	}
	return nodes, err
}

// GetAgentsFromMaster returns a list of agent nodes.
func (pt *DcosPuller) GetAgentsFromMaster() (nodes []Node, err error) {
	retries := []agentResponder{
		&dnsResponder{
			defaultMasterAddress: "leader.mesos",
		},
		&historyServiceResponder{
			defaultPastTime: "/minute/",
		},
		&historyServiceResponder{
			defaultPastTime: "/hour/",
		},
	}
	for _, retry := range retries {
		source, err := retry.getAgentSource()
		if err != nil {
			log.Error(err)
			continue
		}
		nodes, err := retry.getMesosAgents(source)
		if err != nil {
			log.Error(err)
			continue
		}
		if len(nodes) == 0 {
			continue
		}
		return nodes, nil
	}
	return nodes, errors.New("Could not get a list of agent nodes")
}

// WaitBetweenPulls sleep.
func (pt *DcosPuller) WaitBetweenPulls(interval int) {
	time.Sleep(time.Duration(interval) * time.Second)
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
func StartPullWithInterval(config Config, pi Puller, ready chan bool) {
	select {
	case r := <-ready:
		if r == true {
			log.Info(fmt.Sprintf("Start pulling with interval %d", config.FlagPullInterval))
			for {
				runPull(config.FlagPullInterval, config.FlagPort, pi)
			}

		}
	case <-time.After(time.Second * 10):
		log.Error("Not ready to pull from localhost after 10 seconds")
		for {
			runPull(config.FlagPullInterval, config.FlagPort, pi)
		}
	}
}

func runPull(sec int, port int, pi Puller) {
	var ClusterHosts []Node
	masterNodes, err := pi.LookupMaster()
	if err != nil {
		log.Error(err)
		log.Warningf("Could not get a list of master nodes, waiting %d sec", sec)
		pi.WaitBetweenPulls(sec)
		return
	}

	agentNodes, err := pi.GetAgentsFromMaster()
	if err != nil {
		log.Error(err)
		log.Warningf("Could not get a list of agent nodes, waiting %d sec", sec)
		pi.WaitBetweenPulls(sec)
		return
	}

	ClusterHosts = append(ClusterHosts, masterNodes...)
	ClusterHosts = append(ClusterHosts, agentNodes...)

	respChan := make(chan *httpResponse, len(ClusterHosts))
	hostsChan := make(chan Node, len(ClusterHosts))
	loadJobs(hostsChan, ClusterHosts)

	// Pull data from each host
	for i := 0; i <= len(ClusterHosts); i++ {
		go pullHostStatus(hostsChan, respChan, port, pi)
	}

	// blocking here got get all responses from hosts
	clusterHTTPResponses := collectResponses(respChan, len(ClusterHosts))

	// update collected units/nodes health statuses
	updateHealthStatus(clusterHTTPResponses, pi)

	log.Debug(fmt.Sprintf("Waiting %d seconds before next pull", sec))
	pi.WaitBetweenPulls(sec)
}

// function builds a map of all unique units with status
func updateHealthStatus(responses []*httpResponse, pi Puller) {
	units := make(map[string]*unit)
	nodes := make(map[string]*Node)

	for httpResponseIndex, httpResponse := range responses {
		if httpResponse.Status != 200 {
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

func pullHostStatus(hosts <-chan Node, respChan chan<- *httpResponse, port int, pi Puller) {
	for host := range hosts {
		var response httpResponse

		// UnitsRoute available in router.go
		url := fmt.Sprintf("http://%s:%d%s", host.IP, port, BaseRoute)

		// Make a request to get node units status
		// use fake interface implementation for tests
		body, statusCode, err := pi.GetUnitsPropertiesViaHTTP(url)
		if err != nil {
			log.Error(err)
			response.Status = 500
			host.Health = 3 // 3 stands for unknown
			respChan <- &response
			response.Node = host
			continue
		}

		// Response should be strictly mapped to jsonBodyStruct, otherwise skip it
		var jsonBody UnitsHealthResponseJSONStruct
		if err := json.Unmarshal(body, &jsonBody); err != nil {
			log.Errorf("Coult not deserialize json reponse from %s, url: %s", host.IP, url)
			response.Status = 500
			host.Health = 3 // 3 stands for unknown
			respChan <- &response
			response.Node = host
			continue
		}
		response.Status = statusCode

		// Update Response and send it back to respChan
		response.Host = jsonBody.Hostname

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
				pi.GetTimestamp(),
				propertiesMap.PrettyName,
			})
		}
		response.Node = host
		respChan <- &response
	}
}

func loadJobs(jobChan chan Node, hosts []Node) {
	for _, u := range hosts {
		log.Debugf("Adding host to jobs processing channel %+v", u)
		jobChan <- u
	}
	// we should close the channel, since we will recreate a list every 60 sec
	close(jobChan)
}
