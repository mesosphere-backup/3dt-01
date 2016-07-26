package api

import (
	"fmt"
)

func findAgentsInHistoryServiceSelfTest(pastTime string) error {
	finder := &findAgentsInHistoryService{
		pastTime: pastTime,
		next:     nil,
	}
	nodes, err := finder.find()
	if err != nil {
		return err
	}

	if len(nodes) == 0 {
		return fmt.Errorf("No nodes found in history service for past %s", pastTime)
	}

	return nil
}

func findAgentsInHistoryServicePastMinuteSelfTest() error {
	return findAgentsInHistoryServiceSelfTest("/minute/")
}

func findAgentsInHistoryServicePastHourSelfTest() error {
	return findAgentsInHistoryServiceSelfTest("/hour/")
}

func dummySelfTest() error {
	return nil
}

func getSelfTests() map[string]func() error {
	tests := make(map[string]func() error)
	tests["findAgentsInHistoryServicePastMinuteSelfTest"] = findAgentsInHistoryServicePastMinuteSelfTest
	tests["findAgentsInHistoryServicePastHourSelfTest"] = findAgentsInHistoryServicePastHourSelfTest
	tests["dummyTest"] = dummySelfTest
	return tests
}

type selfTestResponse struct {
	Success      bool
	ErrorMessage string
}

func runSelfTest() map[string]*selfTestResponse {
	result := make(map[string]*selfTestResponse)
	for selfTestName, fn := range getSelfTests() {
		result[selfTestName] = &selfTestResponse{}
		err := fn()
		if err == nil {
			result[selfTestName].Success = true
		} else {
			result[selfTestName].ErrorMessage = err.Error()
		}
	}
	return result
}
