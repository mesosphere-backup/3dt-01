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
	dt     *Dt
}

func (s *PullerTestSuit) SetupTest() {
	s.assert = assertPackage.New(s.T())
	s.dt = &Dt{
		DtDCOSTools: &fakeDCOSTools{},
		Cfg:         testCfg,
		MR:          &MonitoringResponse{},
	}
	runPull(s.dt)
}

func (s *PullerTestSuit) TearDownTest() {
	s.dt.MR.UpdateMonitoringResponse(&MonitoringResponse{})
}

// TestMonitoringResponseRace runner that the various exported methods
// of the MonitoringResponse don't race. It does so by calling the methods
// concurrently and will fail under the race detector if the methods race.
func (s *PullerTestSuit) TestMonitoringResponseRace() {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.UpdateMonitoringResponse(&MonitoringResponse{})
	}()
	// We call globalMonitoringResponse twice to ensure the RWMutex's write lock
	// is held, not just a read lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.UpdateMonitoringResponse(&MonitoringResponse{})
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetAllUnits()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetUnit("test-Unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetNodesForUnit("test-Unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetSpecificNodeForUnit("node-ip", "test-Unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetNodes()
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetNodeByID("test-Unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetNodeUnitsID("test-Unit")
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.dt.MR.GetNodeUnitByNodeIDUnitID("test-ip", "test-Unit")
	}()
	wg.Wait()
}

func (s *PullerTestSuit) TestPullerFindUnit() {
	// dcos-master.service should be in monitoring responses
	unit, err := s.dt.MR.GetUnit("dcos-master.service")
	s.assert.Nil(err)
	s.assert.Equal(unit, UnitResponseFieldsStruct{
		"dcos-master.service",
		"PrettyName",
		0,
		"Nice Master Description.",
	})
}

func (s *PullerTestSuit) TestPullerNotFindUnit() {
	// dcos-service-not-here.service should not be in responses
	unit, err := s.dt.MR.GetUnit("dcos-service-not-here.service")
	s.assert.Error(err)
	s.assert.Equal(unit, UnitResponseFieldsStruct{})
}

func TestPullerTestSuit(t *testing.T) {
	suite.Run(t, new(PullerTestSuit))
}
