package api

import (
	"encoding/json"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"net/http"
)

// Route handlers
// /api/v1/system/health, get a units status, used by 3dt puller
func unitsHealthStatus(w http.ResponseWriter, r *http.Request, config *Config) {
	if err := json.NewEncoder(w).Encode(GlobalHealthReport.GetHealthReport()); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/units, get an array of all units collected from all hosts in a cluster
func getAllUnitsHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(GlobalMonitoringResponse.GetAllUnits()); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/units/:unit_id:
func getUnitByIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	unitResponse, err := GlobalMonitoringResponse.GetUnit(vars["unitid"])
	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}
	if err := json.NewEncoder(w).Encode(unitResponse); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/units/:unit_id:/nodes
func getNodesByUnitIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodesForUnitResponse, err := GlobalMonitoringResponse.GetNodesForUnit(vars["unitid"])
	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}
	if err := json.NewEncoder(w).Encode(nodesForUnitResponse); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/units/:unit_id:/nodes/:node_id:
func getNodeByUnitIdNodeIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodePerUnit, err := GlobalMonitoringResponse.GetSpecificNodeForUnit(vars["unitid"], vars["nodeid"])

	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}
	if err := json.NewEncoder(w).Encode(nodePerUnit); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// list the entire tree
func reportHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(GlobalMonitoringResponse); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/nodes
func getNodesHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(GlobalMonitoringResponse.GetNodes()); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/nodes/:node_id:
func getNodeByIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodes, err := GlobalMonitoringResponse.GetNodeById(vars["nodeid"])
	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}

	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// /api/v1/system/health/nodes/:node_id:/units
func getNodeUnitsByNodeIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	units, err := GlobalMonitoringResponse.GetNodeUnitsId(vars["nodeid"])
	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}

	if err := json.NewEncoder(w).Encode(units); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

func getNodeUnitByNodeIdUnitIdHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	unit, err := GlobalMonitoringResponse.GetNodeUnitByNodeIdUnitId(vars["nodeid"], vars["unitid"])
	if err != nil {
		log.Error(err)
		json.NewEncoder(w).Encode(err)
		return
	}
	if err := json.NewEncoder(w).Encode(unit); err != nil {
		log.Error("Failed to encode responses to json")
	}
}
