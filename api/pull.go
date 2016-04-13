package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

	log "github.com/Sirupsen/logrus"
)

var GlobalMonitoringResponse MonitoringResponse

//Puller Interface implementation
type PullType struct {}

func (pt *PullType) GetTimestamp() time.Time {
	return time.Now()
}

func (pt *PullType) GetUnitsPropertiesViaHttp(url string) ([]byte, int, error) {
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

func (pt *PullType) GetAgentsFromMaster() (nodes []Node, err error) {
	// a list of all agents is available from a leader
	leaderIp, err := net.LookupHost("leader.mesos")
	if err != nil {
		log.Error("Unable to resolve mesos leader to get a list of agents")
		return nodes, err
	}
	log.Debugf("Resolved mesos leader ip: %s", leaderIp)
	agentRequest := fmt.Sprintf("http://%s:5050/slaves", leaderIp)

	// Make our request for to /slaves endpoint, return clusterHosts if failed.
	timeout := time.Duration(time.Second)
	client := http.Client{Timeout: timeout}
	getAgents, err := client.Get(agentRequest)
	if err != nil {
		log.Error(fmt.Sprintf("Unable to reach %s - can not determine cluster slave IPs.", agentRequest))
		return nodes, err
	}
	defer getAgents.Body.Close()

	// Read in the response body from the /slaves endpoint. Return clusterHosts if it fails.
	agents, err := ioutil.ReadAll(getAgents.Body)
	if err != nil {
		log.Errorf("Unabled to read the response from %s", agentRequest)
		return nodes, err
	}

	// Create a local instance of our interface{} to unmarshal the JSON response into
	// TODO(mnaboka): Something is not right here, why do we have AgentsResponse
	var sr AgentsResponse
	if err := json.Unmarshal([]byte(agents), &sr); err != nil {
		log.Error("Could not deserialize agents response")
		return nodes, err
	}
	log.Debug(fmt.Sprintf("Slaves response from: %s", sr))
	for _, agent := range sr.Agents {
		var host Node
		host.Role = "agent"
		host.Ip = agent.Hostname
		nodes = append(nodes, host)
	}
	return nodes, nil
}

func (pt *PullType) WaitBetweenPulls(interval int) {
	time.Sleep(time.Duration(interval) * time.Second)
}

func (mr *MonitoringResponse) UpdateMonitoringResponse(r MonitoringResponse) {
	mr.RLock()
	defer mr.RUnlock()
	mr.Nodes = r.Nodes
	mr.Units = r.Units
}

// Get all units available in GlobalMonitoringResponse
func (mr *MonitoringResponse) GetAllUnits() UnitsResponseJsonStruct {
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
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
	mr.Lock()
	defer mr.Unlock()
	if _, ok := mr.Nodes[nodeIp]; !ok {
		return UnitHealthResponseFieldsStruct{}, errors.New(fmt.Sprintf("Node %s not found", nodeIp))
	}
	for _, unit := range mr.Nodes[nodeIp].Units {
		if unit.UnitName == unitId {
			help_field := fmt.Sprintf("Node available at `dcos node ssh -mesos-id %s`. Try, `journalctl -xv` to diagnose further.", mr.Nodes[nodeIp].MesosId)
			return UnitHealthResponseFieldsStruct{
				UnitId: unit.UnitName,
				UnitHealth: unit.Health,
				UnitOutput: mr.Nodes[nodeIp].Output[unit.UnitName],
				UnitTitle: unit.Title,
				Help: help_field,
				PrettyName: unit.PrettyName,
			}, nil
		}
	}
	return UnitHealthResponseFieldsStruct{}, errors.New(fmt.Sprintf("Unit %s not found", unitId))
}

// entry point to pull
func StartPullWithInterval(config Config, pi Puller, ready chan bool) {
	select {
	case r := <-ready:
		if r == true {
			log.Info(fmt.Sprintf("Start pulling with interval %d", config.FlagPullInterval))
			for {
				RunPull(config.FlagPullInterval, config.FlagPort, pi)
			}

		}
	case <-time.After(time.Second * 10):
		log.Error("Not ready to pull from localhost after 10 seconds")
		for {
			RunPull(config.FlagPullInterval, config.FlagPort, pi)
		}
	}
}

func RunPull(sec int, port int, pi Puller) {
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
	UpdateHealthStatus(ClusterHttpResponses, pi)

	log.Debug(fmt.Sprintf("Waiting %d seconds before next pull", sec))
	pi.WaitBetweenPulls(sec)
}

// function builds a map of all unique units with status
func UpdateHealthStatus(responses []*HttpResponse, pi Puller) {
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
		body, statusCode, err := pi.GetUnitsPropertiesViaHttp(url)
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
		for _, propertiesMap := range jsonBody.Array {
			// update node health, if any unit fails, node is unhealthy
			if propertiesMap.UnitHealth > host.Health {
				host.Health = propertiesMap.UnitHealth
			}

			// update error message per host per unit
			host.Output[propertiesMap.UnitId] = propertiesMap.UnitOutput

			response.Units = append(response.Units, Unit{
				propertiesMap.UnitId,
				[]*Node{&host},
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
