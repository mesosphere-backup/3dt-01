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

var GlobalMonitoringResponse MonitoringResponse

//Puller Interface implementation
type PullType struct{}

func (pt *PullType) GetTimestamp() time.Time {
	return time.Now()
}

func makeRequest(timeout time.Duration, req *http.Request) (resp *http.Response, err error) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err = client.Do(req)
	if err != nil {
		return resp, err
	}
	// the user of this function is responsible to close the response.
	return resp, nil
}

func (pt *PullType) GetHttp(url string) (body []byte, statusCode int, err error) {
	request, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return body, 500, err
	}
	timeout := time.Duration(time.Second*3)
	resp, err := makeRequest(timeout, request)
	if err != nil {
		return body, 500, err
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	return body, resp.StatusCode, nil
}

func (pt *PullType) LookupMaster() (nodes_response []Node, err error) {
	url := "http://127.0.0.1:8181/exhibitor/v1/cluster/status"
	client := http.Client{Timeout: time.Duration(time.Second)}
	resp, err := client.Get(url)
	if err != nil {
		log.Errorf("Could not get a list of masters, url %s", url)
		return nodes_response, err
	}
	defer resp.Body.Close()
	nodes_response_string, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Unable to read the response from %s", url)
		return nodes_response, err
	}

	exhibitor_nodes_response := make([]ExhibitorNodeResponse, 0)
	if err := json.Unmarshal([]byte(nodes_response_string), &exhibitor_nodes_response); err != nil {
		log.Error("Could not deserialize nodes response")
		return nodes_response, err
	}
	log.Debugf("Nodes response from exhibitor: %+v", exhibitor_nodes_response)

	for _, exhibitor_node_response := range exhibitor_nodes_response {
		var node Node
		node.Role = "master"
		node.Ip = exhibitor_node_response.Hostname
		node.Leader = exhibitor_node_response.IsLeader
		nodes_response = append(nodes_response, node)
	}
	return nodes_response, nil
}

// agentResponder interface implementation to get a list of agents from a leader master (dns based) or from
// history service.
type dnsResponder struct {
	defaultMasterAddress string
}

type historyServiceResponder struct {
	defaultPastTime string
}

func (hs historyServiceResponder) getAgentSource() (jsonFiles []string, err error) {
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

func (hs historyServiceResponder) getMesosAgents(jsonPath []string) (nodes []Node, err error) {
	nodeCount := make(map[string]int)

	for _, historyFile := range jsonPath {
		agents, err := ioutil.ReadFile(historyFile)
		if err != nil {
			log.Error(err)
			continue
		}

		unquoted_agents, err := strconv.Unquote(string(agents))
		if err != nil {
			log.Error(err)
			continue
		}

		var sr AgentsResponse
		if err := json.Unmarshal([]byte(unquoted_agents), &sr); err != nil {
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
	for ip, _ := range nodeCount {
		var node Node
		node.Role = "agent"
		node.Ip = ip
		nodes = append(nodes, node)
	}
	return nodes, nil
}

func (dr dnsResponder) getAgentSource() (leaderIps []string, err error) {
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

func (dt dnsResponder) getMesosAgents(leaderIp []string) (nodes []Node, err error) {
	agentRequest := fmt.Sprintf("http://%s:5050/slaves", leaderIp)
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

	var sr AgentsResponse
	if err := json.Unmarshal([]byte(agents), &sr); err != nil {
		return nodes, err
	}

	for _, agent := range sr.Agents {
		var node Node
		node.Role = "agent"
		node.Ip = agent.Hostname
		nodes = append(nodes, node)
	}
	return nodes, err
}

func (pt *PullType) GetAgentsFromMaster() (nodes []Node, err error) {
	retries := []agentResponder{
		dnsResponder{
			defaultMasterAddress: "leader.mesos",
		},
		historyServiceResponder{
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
		return nodes, nil
	}
	return nodes, errors.New("Could not get a list of agent nodes")
}

func (pt *PullType) WaitBetweenPulls(interval int) {
	time.Sleep(time.Duration(interval) * time.Second)
}

func (mr *MonitoringResponse) UpdateMonitoringResponse(r MonitoringResponse) {
	mr.Lock()
	defer mr.Unlock()
	mr.Nodes = r.Nodes
	mr.Units = r.Units
}

// Get all units available in GlobalMonitoringResponse
func (mr *MonitoringResponse) GetAllUnits() UnitsResponseJsonStruct {
	mr.RLock()
	defer mr.RUnlock()
	return UnitsResponseJsonStruct{
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

// Get a specific unit available in GlobalMonitoringResponse
func (mr *MonitoringResponse) GetUnit(unitName string) (UnitResponseFieldsStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return UnitResponseFieldsStruct{}, errors.New(fmt.Sprintf("Unit %s not found", unitName))
	}

	return UnitResponseFieldsStruct{
		mr.Units[unitName].UnitName,
		mr.Units[unitName].PrettyName,
		mr.Units[unitName].Health,
		mr.Units[unitName].Title,
	}, nil

}

// Get all hosts for a specific unit available in GlobalMonitoringResponse
func (mr *MonitoringResponse) GetNodesForUnit(unitName string) (NodesResponseJsonStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return NodesResponseJsonStruct{}, errors.New(fmt.Sprintf("Unit %s not found", unitName))
	}
	return NodesResponseJsonStruct{
		Array: func() []*NodeResponseFieldsStruct {
			var r []*NodeResponseFieldsStruct
			for _, node := range mr.Units[unitName].Nodes {
				r = append(r, &NodeResponseFieldsStruct{
					node.Ip,
					node.Health,
					node.Role,
				})
			}
			return r
		}(),
	}, nil
}

func (mr *MonitoringResponse) GetSpecificNodeForUnit(unitName string, nodeIp string) (NodeResponseFieldsWithErrorStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Units[unitName]; !ok {
		return NodeResponseFieldsWithErrorStruct{}, errors.New(fmt.Sprintf("Unit %s not found", unitName))
	}

	for _, node := range mr.Units[unitName].Nodes {
		if node.Ip == nodeIp {
			help_field := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", node.MesosId)

			return NodeResponseFieldsWithErrorStruct{
				node.Ip,
				node.Health,
				node.Role,
				node.Output[unitName],
				help_field,
			}, nil
		}
	}
	return NodeResponseFieldsWithErrorStruct{}, errors.New(fmt.Sprintf("Node %s not found"))
}

func (mr *MonitoringResponse) GetNodes() NodesResponseJsonStruct {
	mr.RLock()
	defer mr.RUnlock()
	return NodesResponseJsonStruct{
		Array: func() []*NodeResponseFieldsStruct {
			var nodes []*NodeResponseFieldsStruct
			for _, node := range mr.Nodes {
				nodes = append(nodes, &NodeResponseFieldsStruct{
					node.Ip,
					node.Health,
					node.Role,
				})
			}
			return nodes
		}(),
	}
}

func (mr *MonitoringResponse) GetNodeById(nodeIp string) (NodeResponseFieldsStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIp]; !ok {
		return NodeResponseFieldsStruct{}, errors.New(fmt.Sprintf("Node %s not found", nodeIp))
	}
	return NodeResponseFieldsStruct{
		mr.Nodes[nodeIp].Ip,
		mr.Nodes[nodeIp].Health,
		mr.Nodes[nodeIp].Role,
	}, nil
}

func (mr *MonitoringResponse) GetNodeUnitsId(nodeIp string) (UnitsResponseJsonStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIp]; !ok {
		return UnitsResponseJsonStruct{}, errors.New(fmt.Sprintf("Node %s not found", nodeIp))
	}
	return UnitsResponseJsonStruct{
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
		}(nodeIp),
	}, nil
}

func (mr *MonitoringResponse) GetNodeUnitByNodeIdUnitId(nodeIp string, unitId string) (UnitHealthResponseFieldsStruct, error) {
	mr.RLock()
	defer mr.RUnlock()
	if _, ok := mr.Nodes[nodeIp]; !ok {
		return UnitHealthResponseFieldsStruct{}, errors.New(fmt.Sprintf("Node %s not found", nodeIp))
	}
	for _, unit := range mr.Nodes[nodeIp].Units {
		if unit.UnitName == unitId {
			help_field := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", mr.Nodes[nodeIp].MesosId)
			return UnitHealthResponseFieldsStruct{
				UnitId:     unit.UnitName,
				UnitHealth: unit.Health,
				UnitOutput: mr.Nodes[nodeIp].Output[unit.UnitName],
				UnitTitle:  unit.Title,
				Help:       help_field,
				PrettyName: unit.PrettyName,
			}, nil
		}
	}
	return UnitHealthResponseFieldsStruct{}, errors.New(fmt.Sprintf("Unit %s not found", unitId))
}

// entry point to pull
func StartPullWithInterval(dt Dt, ready chan bool) {
	select {
	case r := <-ready:
		if r == true {
			log.Info(fmt.Sprintf("Start pulling with interval %d", dt.Cfg.FlagPullInterval))
			for {
				runPull(dt.Cfg.FlagPullInterval, dt.Cfg.FlagPort, dt.DtPuller)
			}

		}
	case <-time.After(time.Second * 10):
		log.Error("Not ready to pull from localhost after 10 seconds")
		for {
			runPull(dt.Cfg.FlagPullInterval, dt.Cfg.FlagPort, dt.DtPuller)
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

	respChan := make(chan *HttpResponse, len(ClusterHosts))
	hostsChan := make(chan Node, len(ClusterHosts))
	loadJobs(hostsChan, ClusterHosts)

	// Pull data from each host
	for i := 0; i <= len(ClusterHosts); i++ {
		go pullHostStatus(hostsChan, respChan, port, pi)
	}

	// blocking here got get all responses from hosts
	ClusterHttpResponses := collectResponses(respChan, len(ClusterHosts))

	// update collected units/nodes health statuses
	updateHealthStatus(ClusterHttpResponses, pi)

	log.Debug(fmt.Sprintf("Waiting %d seconds before next pull", sec))
	pi.WaitBetweenPulls(sec)
}

// function builds a map of all unique units with status
func updateHealthStatus(responses []*HttpResponse, pi Puller) {
	units := make(map[string]*Unit)
	nodes := make(map[string]*Node)

	for httpResponseIndex, httpResponse := range responses {
		if httpResponse.Status != 200 {
			nodes[httpResponse.Node.Ip] = &httpResponse.Node
			log.Errorf("Status code: %d", httpResponse.Status)
			continue
		}
		if _, ok := nodes[httpResponse.Node.Ip]; !ok {
			// copy a node, to avoid circular loop
			newNode := responses[httpResponseIndex].Node
			nodes[httpResponse.Node.Ip] = &newNode
		}
		for unitResponseIndex, unitResponse := range httpResponse.Units {
			// we don't want to have circular explosion here, so make a brand new Unit{} with all fields
			// but []*Node{}
			nodes[httpResponse.Node.Ip].Units = append(nodes[httpResponse.Node.Ip].Units, unitResponse)
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
	GlobalMonitoringResponse.UpdateMonitoringResponse(MonitoringResponse{
		Units: units,
		Nodes: nodes,
	})
}

func collectResponses(respChan <-chan *HttpResponse, totalHosts int) (responses []*HttpResponse) {
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

func pullHostStatus(hosts <-chan Node, respChan chan<- *HttpResponse, port int, pi Puller) {
	for host := range hosts {
		var response HttpResponse

		// UnitsRoute available in router.go
		url := fmt.Sprintf("http://%s:%d%s", host.Ip, port, BaseRoute)

		// Make a request to get node units status
		// use fake interface implementation for tests
		body, statusCode, err := pi.GetHttp(url)
		if err != nil {
			log.Error(err)
			response.Status = 500
			host.Health = 3 // 3 stands for unknown
			respChan <- &response
			response.Node = host
			continue
		}

		// Response should be strictly mapped to jsonBodyStruct, otherwise skip it
		var jsonBody UnitsHealthResponseJsonStruct
		if err := json.Unmarshal(body, &jsonBody); err != nil {
			log.Errorf("Coult not deserialize json reponse from %s, url: %s", host.Ip, url)
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
		host.MesosId = jsonBody.MesosId

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
			host.Output[propertiesMap.UnitId] = propertiesMap.UnitOutput
			response.Units = append(response.Units, Unit{
				propertiesMap.UnitId,
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
