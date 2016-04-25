package api

import (
	"fmt"

	"github.com/gorilla/mux"
	"net/http"
)

const BaseRoute string = "/system/health/v1"

type routeHandler struct {
	url     string
	handler func(http.ResponseWriter, *http.Request)
	headers []header
}

type header struct {
	name  string
	value string
}

func headerMiddleware(next http.Handler, headers []header) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defaultHeaders := []header{
			{
				name:  "Content-type",
				value: "application/json",
			},
		}
		for _, header := range append(defaultHeaders, headers...) {
			w.Header().Add(header.name, header.value)
		}
		next.ServeHTTP(w, r)
	})
}

func getRoutes(config *Config) []routeHandler {
	return []routeHandler{
		{
			// /system/health/v1
			url: BaseRoute,
			handler: func(w http.ResponseWriter, r *http.Request) {
				unitsHealthStatus(w, r, config)
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
	}
}

func loadRoutes(router *mux.Router, config *Config) *mux.Router {
	for _, route := range getRoutes(config) {
		handler := http.HandlerFunc(route.handler)
		router.Handle(route.url, headerMiddleware(handler, route.headers)).Methods("GET")
	}
	return router
}

func NewRouter(config *Config) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	return loadRoutes(router, config)
}
