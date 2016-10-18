package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"os"
	"path/filepath"

	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/mux"
)

func httpError(w http.ResponseWriter, msg string, code int) {
	log.Error(msg)
	http.Error(w, msg, code)
}

// Route handlers
// /api/v1/system/health, get a units status, used by 3dt puller
func unitsHealthStatus(w http.ResponseWriter, r *http.Request, dt Dt) {
	health, err := GetUnitsProperties(dt)
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(health); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/units, get an array of all units collected from all hosts in a cluster
func getAllUnitsHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(globalMonitoringResponse.GetAllUnits()); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/units/:unit_id:
func getUnitByIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	unitResponse, err := globalMonitoringResponse.GetUnit(vars["unitid"])
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(unitResponse); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/units/:unit_id:/nodes
func getNodesByUnitIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodesForUnitResponse, err := globalMonitoringResponse.GetNodesForUnit(vars["unitid"])
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(nodesForUnitResponse); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/units/:unit_id:/nodes/:node_id:
func getNodeByUnitIDNodeIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodePerUnit, err := globalMonitoringResponse.GetSpecificNodeForUnit(vars["unitid"], vars["nodeid"])

	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(nodePerUnit); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// list the entire tree
func reportHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(globalMonitoringResponse); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/nodes
func getNodesHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(globalMonitoringResponse.GetNodes()); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/nodes/:node_id:
func getNodeByIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	nodes, err := globalMonitoringResponse.GetNodeByID(vars["nodeid"])
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(nodes); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// /api/v1/system/health/nodes/:node_id:/units
func getNodeUnitsByNodeIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	units, err := globalMonitoringResponse.GetNodeUnitsID(vars["nodeid"])
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := json.NewEncoder(w).Encode(units); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

func getNodeUnitByNodeIDUnitIDHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	unit, err := globalMonitoringResponse.GetNodeUnitByNodeIDUnitID(vars["nodeid"], vars["unitid"])
	if err != nil {
		httpError(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := json.NewEncoder(w).Encode(unit); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// A helper function to send a response.
func writeResponse(w http.ResponseWriter, response diagnosticsReportResponse) {
	w.WriteHeader(response.ResponseCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

func writeCreateResponse(w http.ResponseWriter, response createResponse) {
	w.WriteHeader(response.ResponseCode)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// diagnostics handlers
// A handler responsible for removing diagnostics bundles. First it will try to find a bundle locally, if failed
// it will send a broadcast request to all cluster master members and check if bundle it available.
// If a bundle was found on a remote host the local node will send a POST request to remove the bundle.
func deleteBundleHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	vars := mux.Vars(r)
	response, err := dt.DtDiagnosticsJob.delete(vars["file"], dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		log.Errorf("Could not delete a file %s: %s", vars["file"], err)
	}
	writeResponse(w, response)
}

// A handler function return a diagnostics job status
func diagnosticsJobStatusHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	if err := json.NewEncoder(w).Encode(dt.DtDiagnosticsJob.getStatus(dt.Cfg)); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// A handler function returns a map of master node ip address as a key and bundleReportStatus as a value.
func diagnosticsJobStatusAllHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	status, err := dt.DtDiagnosticsJob.getStatusAll(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		response, _ := prepareResponseWithErr(http.StatusServiceUnavailable, err)
		writeResponse(w, response)
		return
	}
	if err := json.NewEncoder(w).Encode(status); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// A handler function cancels a job running on a local node first. If a job is running on a remote node
// it will try to send a POST request to cancel it.
func cancelBundleReportHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	response, err := dt.DtDiagnosticsJob.cancel(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		log.Errorf("Could not cancel a job: %s", err)
	}
	writeResponse(w, response)
}

// A handler function returns a map of master ip as a key and a list of bundles as a value.
func listAvailableGLobalBundlesFilesHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	allBundles, err := listAllBundles(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		response, _ := prepareResponseWithErr(http.StatusServiceUnavailable, err)
		writeResponse(w, response)
		return
	}
	if err := json.NewEncoder(w).Encode(allBundles); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// A handler function returns a list of URLs to download bundles
func listAvailableLocalBundlesFilesHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	matches, err := dt.DtDiagnosticsJob.findLocalBundle(dt.Cfg)
	if err != nil {
		response, _ := prepareResponseWithErr(http.StatusServiceUnavailable, err)
		writeResponse(w, response)
		return
	}

	var localBundles []bundle
	for _, file := range matches {
		baseFile := filepath.Base(file)
		fileInfo, err := os.Stat(file)
		if err != nil {
			log.Errorf("Could not stat %s: %s", file, err)
			continue
		}

		localBundles = append(localBundles, bundle{
			File: fmt.Sprintf("%s/report/diagnostics/serve/%s", BaseRoute, baseFile),
			Size: fileInfo.Size(),
		})
	}
	if err := json.NewEncoder(w).Encode(localBundles); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// A handler function serves a static local file. If a file not available locally but
// available on a different node, it will do a reverse proxy.
func downloadBundleHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	vars := mux.Vars(r)
	serveFile := dt.Cfg.FlagDiagnosticsBundleDir + "/" + vars["file"]
	_, err := os.Stat(serveFile)
	if err == nil {
		w.Header().Add("Content-disposition", fmt.Sprintf("attachment; filename=%s", vars["file"]))
		http.ServeFile(w, r, serveFile)
		return
	}
	// do a reverse proxy
	node, location, ok, err := dt.DtDiagnosticsJob.isBundleAvailable(vars["file"], dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if ok {
		director := func(req *http.Request) {
			req = r
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("%s:%d", node, dt.Cfg.FlagPort)
			req.URL.Path = location
		}
		proxy := &httputil.ReverseProxy{Director: director}
		proxy.ServeHTTP(w, r)
		return
	}
	http.NotFound(w, r)
}

// A handler function to start a diagnostics job.
func createBundleHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	var req bundleCreateRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		response, _ := prepareResponseWithErr(http.StatusBadRequest, err)
		writeResponse(w, response)
		return
	}
	response, err := dt.DtDiagnosticsJob.run(req, dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		log.Errorf("Could not run a diagnostics job: %s", err)
	}
	writeCreateResponse(w, response)
}

// A handler function to to get a list of available logs on a node.
func logsListHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	endspoints, err := dt.DtDiagnosticsJob.getLogsEndpoints(dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		response, _ := prepareResponseWithErr(http.StatusServiceUnavailable, err)
		writeResponse(w, response)
		return
	}
	if err := json.NewEncoder(w).Encode(endspoints); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}

// return a log for past N hours for a specific systemd unit
func getUnitLogHandler(w http.ResponseWriter, r *http.Request, dt Dt) {
	vars := mux.Vars(r)
	unitLogOut, err := dt.DtDiagnosticsJob.dispatchLogs(vars["provider"], vars["entity"], dt.Cfg, dt.DtDCOSTools)
	if err != nil {
		response, _ := prepareResponseWithErr(http.StatusServiceUnavailable, err)
		writeResponse(w, response)
		return
	}
	log.Infof("Start read %s", vars["entity"])
	io.Copy(w, unitLogOut)
	log.Infof("Done read %s", vars["entity"])
	unitLogOut.Close()
}

func selfTestHandler(w http.ResponseWriter, r *http.Request) {
	if err := json.NewEncoder(w).Encode(runSelfTest()); err != nil {
		log.Errorf("Failed to encode responses to json: %s", err)
	}
}
