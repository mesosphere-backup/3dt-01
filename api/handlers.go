package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"
)

// Route handlers
func deleteSnapshotHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{}
	vars := mux.Vars(r)
	host, _, ok, err := dt.DtSnapshotJob.isSnapshotAvailable(vars["file"], dt.Cfg, dt.DtPuller)
	if err != nil {
		response.Status = vars["file"] + " not found"
		writeResponse(w, response, http.StatusNotFound)
		return
	}
	if ok {
		if host == fmt.Sprintf("%s:%d", dt.DtHealth.DetectIp(), dt.Cfg.FlagPort) {
			log.Infof("Snapshot %s found on a localhost", vars["file"])
			if err := dt.DtSnapshotJob.DeleteSnapshot(vars["file"], dt.Cfg); err != nil {
				response.Status = err.Error()
				writeResponse(w, response, http.StatusServiceUnavailable)
				return
			}
			response.Status = vars["file"] + " has been sucesfully deleted"
			writeResponse(w, response, http.StatusOK)
			return
		}
		// found a snapshot on a remote host
		url := fmt.Sprintf("http://%s%s/report/snapshot/delete/%s", host, BaseRoute, vars["file"])
		log.Debugf("Remove a file on a remote host, sending POST %s", url)
		req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte{}))
		if err != nil {
			response.Status = err.Error()
			writeResponse(w, response, http.StatusServiceUnavailable)
			return
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			response.Status = err.Error()
			writeResponse(w, response, http.StatusServiceUnavailable)
			return
		}
		defer resp.Body.Close()
		response.Status = "Removed napshot sucessfully " + vars["file"]
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	response.Status = vars["file"] + " was removed during the request"
	http.NotFound(w, r)
}

func statusSnapshotReporthandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	if err := json.NewEncoder(w).Encode(dt.DtSnapshotJob.getStatus(dt.Cfg)); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

func statusAllSnapshotReporthandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{}
	status, err := dt.DtSnapshotJob.getStatusAll(dt.Cfg, dt.DtPuller)
	if err != nil {
		response.Status = err.Error()
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

func cancelSnapshotReportHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{}
	if !dt.DtSnapshotJob.Running {
		response.Status = "Unable to cancel, job is not running"
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	if err := dt.DtSnapshotJob.cancel(dt.DtHealth); err != nil {
		log.Error(err)
		response.Status = "Cancel job failed"
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	response.Status = "Attempting to cancel the job, check status for more details"
	writeResponse(w, response, http.StatusOK)
}

func listAllSnapshotReportHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{}
	allSnapshots, err := listAllSnapshots(dt.Cfg, dt.DtPuller)
	if err != nil {
		response.Status = err.Error()
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	if err := json.NewEncoder(w).Encode(allSnapshots); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

func listSnapshotReportHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{}
	matches, err := dt.DtSnapshotJob.findLocalSnapshot(dt.Cfg)
	if err != nil {
		response.Status = err.Error()
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}

	var snapshots []string
	for _, file := range matches {
		baseFile := filepath.Base(file)
		snapshots = append(snapshots, fmt.Sprintf("%s/report/snapshot/serve/%s", BaseRoute, baseFile))
	}
	if err := json.NewEncoder(w).Encode(snapshots); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

func downloadSnapshotHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	vars := mux.Vars(r)
	serveFile := dt.Cfg.FlagSnapshotDir + "/" + vars["file"]
	_, err := os.Stat(serveFile)
	if err == nil {
		w.Header().Add("Content-disposition", fmt.Sprintf("attachment; filename=%s", vars["file"]))
		http.ServeFile(w, r, serveFile)
		return
	}
	// do a reverse proxy
	host, location, ok, err := dt.DtSnapshotJob.isSnapshotAvailable(vars["file"], dt.Cfg, dt.DtPuller)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok {
		director := func(req *http.Request) {
			req = r
			req.URL.Scheme = "http"
			req.URL.Host = host
			req.URL.Path = location
		}
		proxy := &httputil.ReverseProxy{Director: director}
		proxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

type snapshotCreateRequest struct {
	Version int
	Nodes   []string
}

func writeResponse(w http.ResponseWriter, response snapshotReportResponse, httpStatus int) {
	response.Version = ApiVer
	w.WriteHeader(httpStatus)
	log.Info(response.Status)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Error(err)
	}
}

func createSnapshotReportHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response := snapshotReportResponse{Version: ApiVer}
	var req snapshotCreateRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		response.Status = "could not unmarshal json request"
		writeResponse(w, response, http.StatusBadRequest)
		return
	}
	if err := dt.DtSnapshotJob.run(req, dt.Cfg, dt.DtPuller, dt.DtHealth); err != nil {
		response.Status = err.Error()
		writeResponse(w, response, http.StatusServiceUnavailable)
		return
	}
	response.Status = dt.DtSnapshotJob.Status
	response.Errors = dt.DtSnapshotJob.Errors
	writeResponse(w, response, http.StatusOK)
}

func logsListHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	report, err := getLogsEndpointList(dt.Cfg, dt.DtHealth)
	if err != nil {
		response := snapshotReportResponse{Version: ApiVer}
		response.Status = err.Error()
		writeResponse(w, response, http.StatusNotFound)
		return
	}
	if err := json.NewEncoder(w).Encode(report); err != nil {
		log.Error("Failed to encode responses to json")
	}
}

// return a log for past N hours for a specific systemd unit
func getUnitLogHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	vars := mux.Vars(r)
	doneChan, unitLogOut, err := dispatchLogs(vars["provider"], vars["entity"], dt.Cfg, dt.DtHealth)
	if err != nil {
		response := snapshotReportResponse{Version: ApiVer}
		response.Status = err.Error()
		writeResponse(w, response, http.StatusNotFound)
		return
	}
	io.Copy(w, unitLogOut)
	doneChan <- true
}

// /api/v1/system/health, get a units status, used by 3dt puller
func unitsHealthStatus(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(GlobalUnitsHealthReport.GetHealthReport()); err != nil {
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
