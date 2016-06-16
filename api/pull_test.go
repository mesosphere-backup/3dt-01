package api

import (
	// intentionally rename package to do some magic
	assertPackage "github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"sync"
	"testing"
)

type PullerTestSuit struct {
	suite.Suite
	assert *assertPackage.Assertions
	dt     Dt
}

func (s *PullerTestSuit) SetupTest() {
	s.assert = assertPackage.New(s.T())
	s.dt = Dt{
		DtDCOSTools: &fakeDCOSTools{},
		Cfg: &testCfg,
	}
	runPull(1050, s.dt)
}

func (s *PullerTestSuit) TearDownTest() {
	globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
}

// TestMonitoringResponseRace checks that the various exported methods
// of the MonitoringResponse don't race. It does so by calling the methods
// concurrently and will fail under the race detector if the methods race.
func (s *PullerTestSuit) TestMonitoringResponseRace() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
	}()
	// We call globalMonitoringResponse twice to ensure the RWMutex's write lock
	// is held, not just a read lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.updateMonitoringResponse(monitoringResponse{})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetAllUnits()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodesForUnit("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetSpecificNodeForUnit("node-ip", "test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodes()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeByID("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeUnitsID("test-unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		globalMonitoringResponse.GetNodeUnitByNodeIDUnitID("test-ip", "test-unit")
	}()
	wg.Wait()
}

func (s *PullerTestSuit) TestPullerFindUnit() {
	// dcos-master.service should be in monitoring responses
	unit, err := globalMonitoringResponse.GetUnit("dcos-master.service")
	s.assert.Nil(err)
	s.assert.Equal(unit, unitResponseFieldsStruct{
		"dcos-master.service",
		"PrettyName",
		0,
		"Nice Master Description.",
	})
}

func (s *PullerTestSuit) TestPullerNotFindUnit() {
	// dcos-service-not-here.service should not be in responses
	unit, err := globalMonitoringResponse.GetUnit("dcos-service-not-here.service")
	s.assert.Error(err)
	s.assert.Equal(unit, unitResponseFieldsStruct{})
}

func (s *PullerTestSuit) TestHTTPReqLoadCA() {
	h := HTTPReq{}
	h.Init(&testCfg, &fakeDCOSTools{})
	s.assert.Nil(h.caPool)
}

func TestPullerTestSuit(t *testing.T) {
	suite.Run(t, new(PullerTestSuit))
}
