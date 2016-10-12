package api

import (
	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"net/http"
	"time"
)

// BaseRoute a base 3dt endpoint location.
const BaseRoute string = "/system/health/v1"

type routeHandler struct {
	url                 string
	handler             func(http.ResponseWriter, *http.Request)
	headers             []header
	methods             []string
	gzip, canFlushCache bool
}

type header struct {
	name  string
	value string
}

func headerMiddleware(next http.Handler, headers []header) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("Connection", "close")
		setJSONContentType := true
		for _, header := range headers {
			if header.name == "Content-type" {
				setJSONContentType = false
			}
			w.Header().Add(header.name, header.value)
		}
		if setJSONContentType {
			w.Header().Add("Content-type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func noCacheMiddleware(next http.Handler, dt Dt) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cache := r.URL.Query()["cache"]; len(cache) == 0 {
			next.ServeHTTP(w, r)
			return
		}

		if !dt.Cfg.FlagPull {
			e := "3dt was not started with -pull flag"
			log.Error(e)
			http.Error(w, e, http.StatusServiceUnavailable)
			return
		}

		dt.RunPullerChan <- true
		select {
		case <-dt.RunPullerDoneChan:
			log.Debugf("Successfully collected cluster health status")
			break
		case <-time.After(time.Minute):
			e := "Puller timeout occured"
			log.Error(e)
			http.Error(w, e, http.StatusRequestTimeout)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func getRoutes(dt Dt) []routeHandler {
	return []routeHandler{
		{
			// /system/health/v1
			url: BaseRoute,
			handler: func(w http.ResponseWriter, r *http.Request) {
				unitsHealthStatus(w, r, dt)
			},
		},
		{
			// /system/health/v1/report
			url:           fmt.Sprintf("%s/report", BaseRoute),
			handler:       reportHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/report/download
			url:     fmt.Sprintf("%s/report/download", BaseRoute),
			handler: reportHandler,
			headers: []header{
				{
					name:  "Content-disposition",
					value: "attachment; filename=health-report.json",
				},
			},
			canFlushCache: true,
		},
		{
			// /system/health/v1/units
			url:           fmt.Sprintf("%s/units", BaseRoute),
			handler:       getAllUnitsHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/units/<unitid>
			url:           fmt.Sprintf("%s/units/{unitid}", BaseRoute),
			handler:       getUnitByIDHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/units/<unitid>/nodes
			url:           fmt.Sprintf("%s/units/{unitid}/nodes", BaseRoute),
			handler:       getNodesByUnitIDHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/units/<unitid>/nodes/<nodeid>
			url:           fmt.Sprintf("%s/units/{unitid}/nodes/{nodeid}", BaseRoute),
			handler:       getNodeByUnitIDNodeIDHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/nodes
			url:           fmt.Sprintf("%s/nodes", BaseRoute),
			handler:       getNodesHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/nodes/<nodeid>
			url:           fmt.Sprintf("%s/nodes/{nodeid}", BaseRoute),
			handler:       getNodeByIDHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units
			url:           fmt.Sprintf("%s/nodes/{nodeid}/units", BaseRoute),
			handler:       getNodeUnitsByNodeIDHandler,
			canFlushCache: true,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units/<unitid>
			url:           fmt.Sprintf("%s/nodes/{nodeid}/units/{unitid}", BaseRoute),
			handler:       getNodeUnitByNodeIDUnitIDHandler,
			canFlushCache: true,
		},

		// diagnostics routes
		{
			// /system/health/v1/logs
			url: BaseRoute + "/logs",
			handler: func(w http.ResponseWriter, r *http.Request) {
				logsListHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/logs/<unitid/<hours>
			url: BaseRoute + "/logs/{provider}/{entity}",
			handler: func(w http.ResponseWriter, r *http.Request) {
				getUnitLogHandler(w, r, dt)
			},
			headers: []header{
				{
					name:  "Content-type",
					value: "text/html",
				},
			},
			gzip: true,
		},
		{
			// /system/health/v1/report/diagnostics
			url: BaseRoute + "/report/diagnostics/create",
			handler: func(w http.ResponseWriter, r *http.Request) {
				createBundleHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
		{
			url: BaseRoute + "/report/diagnostics/cancel",
			handler: func(w http.ResponseWriter, r *http.Request) {
				cancelBundleReportHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
		{
			url: BaseRoute + "/report/diagnostics/status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				diagnosticsJobStatusHandler(w, r, dt)
			},
		},
		{
			url: BaseRoute + "/report/diagnostics/status/all",
			handler: func(w http.ResponseWriter, r *http.Request) {
				diagnosticsJobStatusAllHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/diagnostics/list
			url: BaseRoute + "/report/diagnostics/list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				listAvailableLocalBundlesFilesHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/diagnostics/list/all
			url: BaseRoute + "/report/diagnostics/list/all",
			handler: func(w http.ResponseWriter, r *http.Request) {
				listAvailableGLobalBundlesFilesHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/diagnostics/serve/<file>
			url: BaseRoute + "/report/diagnostics/serve/{file}",
			handler: func(w http.ResponseWriter, r *http.Request) {
				downloadBundleHandler(w, r, dt)
			},
			headers: []header{
				{
					name:  "Content-type",
					value: "application/octet-stream",
				},
			},
		},
		{
			// /system/health/v1/report/diagnostics/delete/<file>
			url: BaseRoute + "/report/diagnostics/delete/{file}",
			handler: func(w http.ResponseWriter, r *http.Request) {
				deleteBundleHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
		// self test route
		{
			url:     BaseRoute + "/selftest/info",
			handler: selfTestHandler,
		},
	}
}

func wrapHandler(handler http.Handler, route routeHandler, dt Dt) http.Handler {
	h := headerMiddleware(handler, route.headers)
	if route.gzip {
		h = handlers.CompressHandler(h)
	}
	if route.canFlushCache {
		h = noCacheMiddleware(h, dt)
	}

	return h
}

func loadRoutes(router *mux.Router, dt Dt) *mux.Router {
	for _, route := range getRoutes(dt) {
		if len(route.methods) == 0 {
			route.methods = []string{"GET"}
		}
		handler := http.HandlerFunc(route.handler)
		router.Handle(route.url, wrapHandler(handler, route, dt)).Methods(route.methods...)
	}
	return router
}

// NewRouter returns a new *mux.Router with loaded routes.
func NewRouter(dt Dt) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	return loadRoutes(router, dt)
}
