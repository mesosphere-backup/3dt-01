package api

import (
	"net/http"

	"fmt"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

const ApiVer int = 1
const BaseRoute string = "/system/health/v1"

type routeHandler struct {
	url     string
	handler func(http.ResponseWriter, *http.Request)
	headers []header
	methods []string
	gzip    bool
}

type header struct {
	name  string
	value string
}

func headerMiddleware(next http.Handler, headers []header) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		setJsonContentType := true
		for _, header := range headers {
			if header.name == "Content-type" {
				setJsonContentType = false
			}
			w.Header().Add(header.name, header.value)
		}
		if setJsonContentType {
			w.Header().Add("Content-type", "application/json")
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
				unitsHealthStatus(w, r)
			},
		},
		{
			// /system/health/v1/report
			url:     fmt.Sprintf("%s/report", BaseRoute),
			handler: reportHandler,
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
		},
		{
			// /system/health/v1/units
			url:     fmt.Sprintf("%s/units", BaseRoute),
			handler: getAllUnitsHandler,
		},
		{
			// /system/health/v1/units/<unitid>
			url:     fmt.Sprintf("%s/units/{unitid}", BaseRoute),
			handler: getUnitByIdHandler,
		},
		{
			// /system/health/v1/units/<unitid>/nodes
			url:     fmt.Sprintf("%s/units/{unitid}/nodes", BaseRoute),
			handler: getNodesByUnitIdHandler,
		},
		{
			// /system/health/v1/units/<unitid>/nodes/<nodeid>
			url:     fmt.Sprintf("%s/units/{unitid}/nodes/{nodeid}", BaseRoute),
			handler: getNodeByUnitIdNodeIdHandler,
		},
		{
			// /system/health/v1/nodes
			url:     fmt.Sprintf("%s/nodes", BaseRoute),
			handler: getNodesHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>
			url:     fmt.Sprintf("%s/nodes/{nodeid}", BaseRoute),
			handler: getNodeByIdHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units
			url:     fmt.Sprintf("%s/nodes/{nodeid}/units", BaseRoute),
			handler: getNodeUnitsByNodeIdHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units/<unitid>
			url:     fmt.Sprintf("%s/nodes/{nodeid}/units/{unitid}", BaseRoute),
			handler: getNodeUnitByNodeIdUnitIdHandler,
		},
		{
			// /system/health/v1/logs
			url: BaseRoute + "/logs",
			handler: func(w http.ResponseWriter, r *http.Request) {
				logsListHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/logs/<unitid/<hours>
			url:     BaseRoute + "/logs/{provider}/{entity}",
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
			// /system/health/v1/report/snapshot
			url: BaseRoute + "/report/snapshot/create",
			handler: func(w http.ResponseWriter, r *http.Request) {
				createSnapshotReportHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
		{
			url: BaseRoute + "/report/snapshot/cancel",
			handler: func(w http.ResponseWriter, r *http.Request) {
				cancelSnapshotReportHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
		{
			url: BaseRoute + "/report/snapshot/status",
			handler: func(w http.ResponseWriter, r *http.Request) {
				statusSnapshotReporthandler(w, r, dt)
			},
		},
		{
			url: BaseRoute + "/report/snapshot/status/all",
			handler: func(w http.ResponseWriter, r *http.Request) {
				statusAllSnapshotReporthandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/snapshot/list
			url: BaseRoute + "/report/snapshot/list",
			handler: func(w http.ResponseWriter, r *http.Request) {
				listSnapshotReportHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/snapshot/list/all
			url: BaseRoute + "/report/snapshot/list/all",
			handler: func(w http.ResponseWriter, r *http.Request) {
				listAllSnapshotReportHandler(w, r, dt)
			},
		},
		{
			// /system/health/v1/report/snapshot/serve/<file>
			url: BaseRoute + "/report/snapshot/serve/{file}",
			handler: func(w http.ResponseWriter, r *http.Request) {
				downloadSnapshotHandler(w, r, dt)
			},
			headers: []header{
				{
					name:  "Content-type",
					value: "application/octet-stream",
				},
			},
		},
		{
			// /system/health/v1/report/snapshot/delete/<file>
			url: BaseRoute + "/report/snapshot/delete/{file}",
			handler: func(w http.ResponseWriter, r *http.Request) {
				deleteSnapshotHandler(w, r, dt)
			},
			methods: []string{"POST"},
		},
	}
}

func wrapHandler(handler http.Handler, route routeHandler) http.Handler {
	handlerWithHeader := headerMiddleware(handler, route.headers)
	if route.gzip {
		return handlers.CompressHandler(handlerWithHeader)
	}
	return handlerWithHeader
}

func loadRoutes(router *mux.Router, dt Dt) *mux.Router {
	for _, route := range getRoutes(dt) {
		if len(route.methods) == 0 {
			route.methods = []string{"GET"}
		}
		handler := http.HandlerFunc(route.handler)
		router.Handle(route.url, wrapHandler(handler, route)).Methods(route.methods...)
	}
	return router
}

func NewRouter(dt Dt) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	return loadRoutes(router, dt)
}
