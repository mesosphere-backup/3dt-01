package api_test

import (
	. "github.com/dcos/3dt/api"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"errors"
)

// FakeSystemdType success
type FakeSystemdType struct {
	newCalled bool
	params    map[string]bool
	units     []string
}

func (st *FakeSystemdType) GetHostname() string {
	return "MyHostName"
}

func (st *FakeSystemdType) DetectIp() string {
	return "127.0.0.1"
}

func (st *FakeSystemdType) GetNodeRole() string {
	return "master"
}

func (st *FakeSystemdType) GetUnitProperties(pname string) (map[string]interface{}, error) {
	result := make(map[string]interface{})
	st.units = append(st.units, pname)
	if pname == "unit_to_fail" {
		return result, errors.New("unit_to_fail occured")
	}
	result["LoadState"] = "loaded"
	result["ActiveState"] = "active"
	result["Description"] = "PrettyName: My fake description"
	return result, nil
}

func (st *FakeSystemdType) InitializeDbusConnection() error {
	return nil
}

func (st *FakeSystemdType) CloseDbusConnection() error {
	return nil
}

func (st *FakeSystemdType) GetUnitNames() (units []string, err error) {
	units = []string{"unit_a", "unit_b", "unit_c", "unit_to_fail"}
	return units, err
}

func (st *FakeSystemdType) GetJournalOutput(unit string) (string, error) {
	return "journal output", nil
}

func (st *FakeSystemdType) GetMesosNodeId(role string, field string) string {
	return "node-id-123"
}

var _ = Describe("Test systemd", func() {
	var cfg Config

	BeforeEach(func() {
		args := []string{"3dt", "test"}
		cfg, _ = LoadDefaultConfig(args, "0.0.7")
		cfg.Systemd = &FakeSystemdType{}
	})

	Context("Succseful return", func() {
		It("should execute GetUnitsProperties() with 6 service and get UnitsHealthResponseJsonStruct with 3 services", func() {
			Expect(GetUnitsProperties(&cfg)).Should(Equal(UnitsHealthResponseJsonStruct{
				Array: []UnitHealthResponseFieldsStruct{
					{
						"unit_a",
						0,
						"",
						"My fake description",
						"",
						"PrettyName",
					},
					{
						"unit_b",
						0,
						"",
						"My fake description",
						"",
						"PrettyName",
					},
					{
						"unit_c",
						0,
						"",
						"My fake description",
						"",
						"PrettyName",
					},
				},
				DcosVersion: "",
				Hostname:    "MyHostName",
				IpAddress:   "127.0.0.1",
				Role:        "master",
				MesosId:     "node-id-123",
				TdtVersion:  "0.0.7",
			}))
			Expect(cfg.SystemdUnits).Should(Equal([]string{
				"dcos-setup.service",
				"dcos-link-env.service",
				"dcos-download.service",
			}))
		})
	})

	Context("Test help functions", func() {
		It("IsInList should return true", func() {
			Expect(IsInList("match", []string{"test", "match", "none"})).Should(Equal(true))
		})

		It("IsInList should return false", func() {
			Expect(IsInList("nomatch", []string{"test", "match", "none"})).Should(Equal(false))
		})

		It("should return healthy unit", func() {
			p := make(map[string]interface{})
			p["LoadState"] = "loaded"
			p["ActiveState"] = "active"
			p["Description"] = "PrettyName: Description"

			Expect(NormalizeProperty("unit_name", p, cfg.Systemd)).Should(Equal(UnitHealthResponseFieldsStruct{
				"unit_name",
				0,
				"",
				"Description",
				"",
				"PrettyName",
			}))
		})

		It("should return unhealthy unit, LoadState cannot be not `loaded`", func() {
			p := make(map[string]interface{})
			p["LoadState"] = "unloaded"
			p["ActiveState"] = "active"
			p["Description"] = "PrettyName: Description"

			Expect(NormalizeProperty("unit_name", p, cfg.Systemd)).Should(Equal(UnitHealthResponseFieldsStruct{
				"unit_name",
				1,
				"unit_name is not loaded. Please check `systemctl show all` to check current unit status. \njournal output",
				"Description",
				"",
				"PrettyName",
			}))
		})

		It("should return unhealthy unit, ActiveState should be `active` or `inactive`", func() {
			p := make(map[string]interface{})
			p["LoadState"] = "loaded"
			p["ActiveState"] = "notactive"
			p["Description"] = "PrettyName: Description"
			Expect(NormalizeProperty("unit_name", p, cfg.Systemd)).Should(Equal(UnitHealthResponseFieldsStruct{
				"unit_name",
				1,
				"unit_name state is not one of the possible states [active inactive activating]. Current state is [ notactive ]. Please check `systemctl show all unit_name` to check current unit state. \njournal output",
				"Description",
				"",
				"PrettyName",
			}))
		})
	})

})
