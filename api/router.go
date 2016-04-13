package api

import (
	"fmt"

	"github.com/gorilla/mux"
	"net/http"
)

const BaseRoute string = "/system/health/v1"

func NewRouter(config *Config) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	// pass config structure to a handler
	router.HandleFunc(BaseRoute, func(w http.ResponseWriter, r *http.Request) {
		UnitsHealthStatus(w, r, config)
	}).Methods("GET")

	// local list of systemd units
	router.HandleFunc(fmt.Sprintf("%s/report", BaseRoute), ReportHandler).Methods("GET")

	// collected list of units
	router.HandleFunc(fmt.Sprintf("%s/units", BaseRoute), GetAllUnitsHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/units/{unitid}", BaseRoute), GetUnitByIdHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/units/{unitid}/nodes", BaseRoute), GetNodesByUnitIdHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/units/{unitid}/nodes/{nodeid}", BaseRoute), GetNodeByUnitIdNodeIdHandler).Methods("GET")

	// collected list of nodes
	router.HandleFunc(fmt.Sprintf("%s/nodes", BaseRoute), GetNodesHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/nodes/{nodeid}", BaseRoute), GetNodeByIdHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/nodes/{nodeid}/units", BaseRoute), GetNodeUnitsByNodeIdHandler).Methods("GET")
	router.HandleFunc(fmt.Sprintf("%s/nodes/{nodeid}/units/{unitid}", BaseRoute), GetNodeUnitByNodeIdUnitIdHandler).Methods("GET")
	return router
}
