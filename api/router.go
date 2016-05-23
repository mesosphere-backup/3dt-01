package api

import (
	"fmt"

	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
	"net/http"
)

// BaseRoute a base 3dt endpoint location.
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
			handler: getUnitByIDHandler,
		},
		{
			// /system/health/v1/units/<unitid>/nodes
			url:     fmt.Sprintf("%s/units/{unitid}/nodes", BaseRoute),
			handler: getNodesByUnitIDHandler,
		},
		{
			// /system/health/v1/units/<unitid>/nodes/<nodeid>
			url:     fmt.Sprintf("%s/units/{unitid}/nodes/{nodeid}", BaseRoute),
			handler: getNodeByUnitIDNodeIDHandler,
		},
		{
			// /system/health/v1/nodes
			url:     fmt.Sprintf("%s/nodes", BaseRoute),
			handler: getNodesHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>
			url:     fmt.Sprintf("%s/nodes/{nodeid}", BaseRoute),
			handler: getNodeByIDHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units
			url:     fmt.Sprintf("%s/nodes/{nodeid}/units", BaseRoute),
			handler: getNodeUnitsByNodeIDHandler,
		},
		{
			// /system/health/v1/nodes/<nodeid>/units/<unitid>
			url:     fmt.Sprintf("%s/nodes/{nodeid}/units/{unitid}", BaseRoute),
			handler: getNodeUnitByNodeIDUnitIDHandler,
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

func loadRoutes(router *mux.Router, config *Config) *mux.Router {
	for _, route := range getRoutes(config) {
		if len(route.methods) == 0 {
			route.methods = []string{"GET"}
		}
		handler := http.HandlerFunc(route.handler)
		router.Handle(route.url, wrapHandler(handler, route)).Methods(route.methods...)
	}
	return router
}

// NewRouter returns a new *mux.Router with loaded routes.
func NewRouter(config *Config) *mux.Router {
	router := mux.NewRouter().StrictSlash(true)
	return loadRoutes(router, config)
}
