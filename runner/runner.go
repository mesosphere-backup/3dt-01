package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/dcos/dcos-go/dcos"
	"github.com/pkg/errors"
)

const (
	statusOK = 0
	statusUnknown = 3
)

// NewRunner returns an initialized instance of *Runner.
func NewRunner(role string) *Runner {
	// according to design doc, runner config must treat roles `agent` and `agent_public`
	// as a single `agent` role. If a user create a config and sets role `agent`, we expect to run this
	// check on both agent and agent_public nodes.
	// https://jira.mesosphere.com/browse/DCOS_OSS-1242
	if role == dcos.RoleAgentPublic {
		role = dcos.RoleAgent
	}
	return &Runner{
		role: role,
	}
}

// Response provides a command Response.
type Response struct {
	name        string
	duration    string
	list        bool

	output      string
	status      int
	description string
	fullCmd     []string
	timeout     string
}

type response struct {
	Output string `json:"output"`
	Status int `json:"status"`
}

type responseList struct {
	Description string `json:"description"`
	FullCmd     []string `json:"full_cmd"`
	Timeout     string `json:"timeout"`
}

// MarshalJSON is a custom json marshaller implementation used to return output based on user request.
// responseList is returned if a user requested to list the checks. response is used to return back the result
// with populated Output and executable return code.
func (r Response) MarshalJSON() ([]byte, error) {
	if r.list {
		return json.Marshal(&responseList{
			Description: r.description,
			FullCmd: r.fullCmd,
			Timeout: r.timeout,
		})
	}

	return json.Marshal(&response{
		Output: r.output,
		Status: r.status,
	})
}

// NewCombinedResponse initiates a new instance of CombinedResponse.
func NewCombinedResponse(list bool) *CombinedResponse {
	return &CombinedResponse{
		checks: make(map[string]*Response),
		errs:   make(map[string]*Response),
		list: list,
	}
}

// CombinedResponse represents a parent structure for multiple checks.
type CombinedResponse struct {
	status        int
	list          bool
	checkNotFound bool
	checks        map[string]*Response
	errs          map[string]*Response
}

// Status returns checks combined status.
func (cr CombinedResponse) Status() int {
	return cr.status
}

// MarshalJSON is a custom json marshaller implementation used to return the appropriate response based
// on user input. combinedResponseError is used to return back error message if runner was unable to execute a check.
// CombinedResponse.Checks is used to return back a list of checks without executing them, combinedResponseSuccess is
// used to return user the actual checks output.
func (cr CombinedResponse) MarshalJSON() ([]byte, error) {
	if len(cr.errs) > 0 {
		var errs []string
		for e, r := range cr.errs {
			errs = append(errs, fmt.Sprintf("%s: %s", r.name, e))
		}

		if cr.checkNotFound {
			return json.Marshal(combinedResponseError{
				Error: "One or more requested checks could not be found on this node.",
				Checks: errs,
			})
		}

		return json.Marshal(combinedResponseError{
			Error: "One or more requested checks failed to execute.",
			Checks: errs,
		})
	}

	if cr.list {
		return json.Marshal(cr.checks)
	}

	return json.Marshal(combinedResponseSuccess{
		Status: cr.status,
		Checks: cr.checks,
	})
}

type combinedResponseSuccess struct {
	Status int `json:"status"`
	Checks map[string]*Response `json:"checks"`
}

type combinedResponseError struct {
	Error string `json:"error"`
	Checks []string `json:"checks"`
}

// Runner is a main instance of DC/OS runner runner.
type Runner struct {
	ClusterChecks map[string]*Check `json:"cluster_checks"`
	NodeChecks    struct {
		Checks    map[string]*Check `json:"checks"`
		PreStart  []string          `json:"prestart"`
		PostStart []string          `json:"poststart"`
	} `json:"node_checks"`

	role string
}

// Load loads values to Runner struct from io.Reader
func (r *Runner) Load(reader io.Reader) error {
	if err := json.NewDecoder(reader).Decode(r); err != nil {
		return errors.Wrap(err, "unable to decode a config file")
	}
	return r.validate()
}

// LoadFromFile opens a config file and try to load the values to Runner struct.
func (r *Runner) LoadFromFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return errors.Wrap(err, "unable to open config file")
	}
	return r.Load(f)
}

func (r *Runner) validate() error {
	return nil
}

// Cluster executes cluster runner defined in config.
func (r *Runner) Cluster(ctx context.Context, list bool, selectiveChecks ...string) (*CombinedResponse, error) {
	return r.run(ctx, r.ClusterChecks, list, r.clusterCheckNames(), selectiveChecks...)
}

func (r *Runner) clusterCheckNames() (clusterChecks []string) {
	for checkName := range r.ClusterChecks {
		clusterChecks = append(clusterChecks, checkName)
	}
	return
}

// PreStart executes the runner defined in config node_checks->prestart.
func (r *Runner) PreStart(ctx context.Context, list bool, selectiveChecks ...string) (*CombinedResponse, error) {
	return r.run(ctx, r.NodeChecks.Checks, list, r.NodeChecks.PreStart, selectiveChecks...)
}

// PostStart executes the runner defined in config node_checks->poststart.
func (r *Runner) PostStart(ctx context.Context, list bool, selectiveChecks ...string) (*CombinedResponse, error) {
	return r.run(ctx, r.NodeChecks.Checks, list, r.NodeChecks.PostStart, selectiveChecks...)
}

func (r *Runner) run(ctx context.Context, checkMap map[string]*Check, list bool, checkList []string, selectiveChecks ...string) (*CombinedResponse, error) {
	max := func(a, b int) int {
		// valid values are 0,1,2,3. All other values should result in 3.
		if (a > statusUnknown || a < statusOK) || (b > statusUnknown || b < statusOK) {
			return statusUnknown
		}

		if a > b {
			return a
		}
		return b
	}

	// if no checks defined, return empty response.
	combinedResponse := NewCombinedResponse(list)
	if len(checkList) == 0 {
		return combinedResponse, nil
	}

	currentCheckList := checkList

	// if a caller passed selectiveChecks, we should make sure those checks are in checkList
	// and use only those.
	if len(selectiveChecks) > 0 {
		currentCheckList = []string{}
		for _, selectiveCheck := range selectiveChecks {
			for _, checkItem := range checkList {
				if selectiveCheck == checkItem {
					currentCheckList = append(currentCheckList, selectiveCheck)
					break
				}
			}
		}
	}
	
	// main loop to get the checks info.
	for _, name := range currentCheckList {
		resp := &Response{
			name: name,
		}

		currentCheck, ok := checkMap[name]
		if !ok {
			combinedResponse.errs["check not found"] = resp
			combinedResponse.checkNotFound = true
			continue
		}

		// find runner for the given role only.
		if !currentCheck.verifyRole(r.role) {
			continue
		}

		var (
			combinedOutput []byte
			code           int
			err            error
			checkDuration  string
		)

		if _, ok := combinedResponse.checks[name]; ok {
			combinedResponse.errs["duplicate check"] = resp
			continue
		}

		// list option disables the check execution
		if !list {

			start := time.Now()
			combinedOutput, code, err = currentCheck.Run(ctx, r.role)
			checkDuration = time.Since(start).String()
		}

		resp.output = string(combinedOutput)
		resp.status = code
		resp.duration = checkDuration
		resp.description =  currentCheck.Description
		resp.fullCmd = currentCheck.Cmd
		resp.timeout = currentCheck.Timeout
		resp.list = list
		combinedResponse.checks[name] = resp

		// collect errors
		if err != nil {
			combinedResponse.errs[err.Error()] = resp
		}

		combinedResponse.status = max(combinedResponse.status, code)
	}

	return combinedResponse, nil
}
