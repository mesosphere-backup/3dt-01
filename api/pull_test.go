package api_test

import (
	. "github.com/mesosphere/3dt/api"

	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

// Fake Interface and implementation for Pulling functionality
type FakePuller struct {
	test              bool
	urls              []string
	fakeHttpResponses []*HttpResponse
}

func (pt *FakePuller) GetTimestamp() time.Time {
	return time.Date(2009, time.November, 10, 23, 0, 0, 0, time.UTC)
}

func (pt *FakePuller) LookupMaster() (nodes []Node, err error) {
	var fakeMasterHost Node
	fakeMasterHost.Ip = "127.0.0.1"
	fakeMasterHost.Role = "master"
	nodes = append(nodes, fakeMasterHost)
	return nodes, nil
}

func (pt *FakePuller) GetAgentsFromMaster() (nodes []Node, err error) {
	var fakeAgentHost Node
	fakeAgentHost.Ip = "127.0.0.2"
	fakeAgentHost.Role = "agent"
	nodes = append(nodes, fakeAgentHost)
	return nodes, nil
}

func (pt *FakePuller) GetUnitsPropertiesViaHttp(url string) ([]byte, int, error) {
	var response string

	// master
	if url == fmt.Sprintf("http://127.0.0.1:1050%s", BaseRoute) {
		response = `
			{
			  "units": [
			    {
			      "id":"dcos-setup.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Description.",
			      "help":"",
			      "name":"PrettyName"
			    },
			    {
			      "id":"dcos-master.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Master Description.",
			      "help":"",
			      "name":"PrettyName"
			    }
			  ],
			  "hostname":"master01",
			  "ip":"127.0.0.1",
			  "dcos_version":"1.6",
			  "node_role":"master",
			  "mesos_id":"master-123",
			  "3dt_version": "0.0.7"
			}`
	}

	// agent
	if url == fmt.Sprintf("http://127.0.0.2:1050%s", BaseRoute) {
		response = `
			{
			  "units": [
			    {
			      "id":"dcos-setup.service",
			      "health":0,
			      "output":"",
			      "description":"Nice Description.",
			      "help":"",
			      "name":"PrettyName"
			    },
			    {
			      "id":"dcos-agent.service",
			      "health":1,
			      "output":"",
			      "description":"Nice Agent Description.",
			      "help":"",
			      "name":"PrettyName"
			    }
			  ],
			  "hostname":"agent01",
			  "ip":"127.0.0.2",
			  "dcos_version":"1.6",
			  "node_role":"agent",
			  "mesos_id":"agent-123",
			  "3dt_version": "0.0.7"
			}`
	}
	return []byte(response), 200, nil
}

func (pt *FakePuller) WaitBetweenPulls(interval int) {
}

func (pt *FakePuller) UpdateHttpResponses(responses []*HttpResponse) {
	pt.fakeHttpResponses = responses
}

var _ = Describe("Test Pull functionality", func() {
	// RunPull only once
	pi := &FakePuller{}
	RunPull(1, 1050, pi)

	Context("Run puller", func() {
		It("should have `dcos-master.service` in GlobalMonitoringResponse.Units", func() {
			//test GetUnit()
			unit, err := GlobalMonitoringResponse.GetUnit("dcos-master.service")
			Expect(err).Should(BeNil())
			Expect(unit).Should(Equal(UnitResponseFieldsStruct{
				"dcos-master.service",
				"PrettyName",
				0,
				"Nice Master Description.",
			}))
		})

		It("should not have `dcos-service-not-here.service` in GlobalMonitoringResponse.Units", func() {
			// test GetUnit()
			unit, err := GlobalMonitoringResponse.GetUnit("dcos-service-not-here.service")
			Expect(err).Should(HaveOccurred())
			Expect(unit).Should(Equal(UnitResponseFieldsStruct{}))
		})

		It("should have 3 units in GlobalMonitoringResponse.GetAllUnits", func() {
			// test GetUnit()
			units := GlobalMonitoringResponse.GetAllUnits()
			Expect(len(units.Array)).Should(Equal(3))
			Expect(units.Array).Should(ContainElement(UnitResponseFieldsStruct{
				"dcos-setup.service",
				"PrettyName",
				0,
				"Nice Description.",
			}))
			Expect(units.Array).Should(ContainElement(UnitResponseFieldsStruct{
				"dcos-master.service",
				"PrettyName",
				0,
				"Nice Master Description.",
			}))
			Expect(units.Array).Should(ContainElement(UnitResponseFieldsStruct{
				"dcos-agent.service",
				"PrettyName",
				1,
				"Nice Agent Description.",
			}))
		})

		It("should have 2 nodes with unit dcos-setup.service", func() {
			// test GetNodesForUnit()
			nodes, err := GlobalMonitoringResponse.GetNodesForUnit("dcos-setup.service")
			Expect(err).Should(BeNil())
			Expect(len(nodes.Array)).Should(Equal(2))
			Expect(nodes.Array).Should(ContainElement(&NodeResponseFieldsStruct{
				"127.0.0.1",
				0,
				"master",
			}))
			Expect(nodes.Array).Should(ContainElement(&NodeResponseFieldsStruct{
				"127.0.0.2",
				1,
				"agent",
			}))
		})

		It("should not have any nodes with unit dcos-non-existed.service", func() {
			// test GetNodesForUnit()
			// should fail
			nodes, err := GlobalMonitoringResponse.GetNodesForUnit("dcos-non-existed.service")
			Expect(err).Should(HaveOccurred())
			Expect(nodes).Should(Equal(NodesResponseJsonStruct{}))
		})

		It("should have 1 node 127.0.0.1 with unit dcos-setup.service", func() {
			// test GetSpecificNodeForUnit()
			node, err := GlobalMonitoringResponse.GetSpecificNodeForUnit("dcos-setup.service", "127.0.0.1")
			Expect(err).Should(BeNil())
			Expect(node).Should(Equal(NodeResponseFieldsWithErrorStruct{
				"127.0.0.1",
				0,
				"master",
				"",
				"Node available at `dcos node ssh -mesos-id master-123`. Try, `journalctl -xv` to diagnose further.",
			}))
		})

		It("should have node 127.0.0.1 with dcos-master.service", func() {
			// test GetSpecificNodeForUnit()
			node, err := GlobalMonitoringResponse.GetSpecificNodeForUnit("dcos-master.service", "127.0.0.1")
			Expect(err).Should(BeNil())
			Expect(node).Should(Equal(NodeResponseFieldsWithErrorStruct{
				"127.0.0.1",
				0,
				"master",
				"",
				"Node available at `dcos node ssh -mesos-id master-123`. Try, `journalctl -xv` to diagnose further.",
			}))
		})

		It("should have node 127.0.0.1 with dcos-agent.service", func() {
			// test GetSpecificNodeForUnit()
			node, err := GlobalMonitoringResponse.GetSpecificNodeForUnit("dcos-agent.service", "127.0.0.2")
			Expect(err).Should(BeNil())
			Expect(node).Should(Equal(NodeResponseFieldsWithErrorStruct{
				"127.0.0.2",
				1,
				"agent",
				"",
				"Node available at `dcos node ssh -mesos-id agent-123`. Try, `journalctl -xv` to diagnose further.",
			}))
		})

		It("should not have node 127.0.0.1 with dcos-agent.service", func() {
			// test GetSpecificNodeForUnit()
			node, err := GlobalMonitoringResponse.GetSpecificNodeForUnit("dcos-agent.service", "127.0.0.1")
			Expect(err).Should(HaveOccurred())
			Expect(node).Should(Equal(NodeResponseFieldsWithErrorStruct{}))
		})

		It("should have 2 nodes in GlobalMonitoringResponse.Nodes", func() {
			// test GetNodes()
			nodes := GlobalMonitoringResponse.GetNodes()
			Expect(len(nodes.Array)).Should(Equal(2))
			Expect(nodes.Array).Should(ContainElement(&NodeResponseFieldsStruct{
				"127.0.0.1",
				0,
				"master",
			}))
			Expect(nodes.Array).Should(ContainElement(&NodeResponseFieldsStruct{
				"127.0.0.2",
				1,
				"agent",
			}))
		})

		It("should have node 127.0.0.1 in GlobalMonitoringResponse.Nodes", func() {
			// test GetNodeById()
			node, err := GlobalMonitoringResponse.GetNodeById("127.0.0.1")
			Expect(err).Should(BeNil())
			Expect(node).Should(Equal(NodeResponseFieldsStruct{
				"127.0.0.1",
				0,
				"master",
			}))
		})

		It("should not have node 10.10.10.10 in GlobalMonitoringResponse.Nodes", func() {
			// test GetNodeById()
			node, err := GlobalMonitoringResponse.GetNodeById("10.10.10.10")
			Expect(err).Should(HaveOccurred())
			Expect(node).Should(Equal(NodeResponseFieldsStruct{}))

		})

		It("should have node 127.0.0.1 with service dcos-master.service in GlobalMonitoringResponse.Nodes", func() {
			// test GetNodeUnitByNodeIdUnitId()
			nodes_by_unit, err := GlobalMonitoringResponse.GetNodeUnitByNodeIdUnitId("127.0.0.1", "dcos-master.service")
			Expect(err).Should(BeNil())
			Expect(nodes_by_unit).Should(Equal(UnitHealthResponseFieldsStruct{
				"dcos-master.service",
				0,
				"",
				"Nice Master Description.",
				"Node available at `dcos node ssh -mesos-id master-123`. Try, `journalctl -xv` to diagnose further.",
				"PrettyName",
			}))
		})

		It("should not have node 127.0.0.2 with service dcos-master.service in GlobalMonitoringResponse.Nodes", func() {
			// test GetNodeUnitByNodeIdUnitId()
			nodes, err := GlobalMonitoringResponse.GetNodeUnitByNodeIdUnitId("127.0.0.2", "dcos-master.service")
			Expect(err).Should(HaveOccurred())
			Expect(nodes).Should(Equal(UnitHealthResponseFieldsStruct{}))
		})
	})
})
