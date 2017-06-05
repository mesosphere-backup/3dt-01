package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/errors"
)

// NewRunner returns an initialized instance of *Runner.
func NewRunner(role string) *Runner {
	return &Runner{
		role: role,
	}
}

// Response provides a command Response.
type Response struct {
	name        string
	duration    string
	list        bool

	output string
	code   int
	description string
	fullCmd []string
	timeout string
}

type response struct {
	Output      string `json:"output"`
	Code        int `json:"code"`
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
		Code: r.code,
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
	status int
	list   bool
	checks map[string]*Response
	errs   map[string]*Response
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
func (r *Runner) Cluster(ctx context.Context, list bool, names ...string) (*CombinedResponse, error) {
	return r.run(ctx, r.ClusterChecks, list, names...)
}

// PreStart executes the runner defined in config node_checks->prestart.
func (r *Runner) PreStart(ctx context.Context, list bool, names ...string) (*CombinedResponse, error) {
	checkNames := r.NodeChecks.PreStart
	// if names is not empty override the prestart runner.
	if len(names) > 0 {
		checkNames = names
	}
	return r.run(ctx, r.NodeChecks.Checks, list, checkNames...)
}

// PostStart executes the runner defined in config node_checks->poststart.
func (r *Runner) PostStart(ctx context.Context, list bool, names ...string) (*CombinedResponse, error) {
	checkNames := r.NodeChecks.PostStart
	// if names is not empty override the poststart runner.
	if len(names) > 0 {
		checkNames = names
	}
	return r.run(ctx, r.NodeChecks.Checks, list, checkNames...)
}

func (r *Runner) run(ctx context.Context, checkMap map[string]*Check, list bool, names ...string) (*CombinedResponse, error) {
	// if not names passed, use all from checkMap
	if len(names) == 0 {
		for n := range checkMap {
			names = append(names, n)
		}
	}

	max := func(a, b int) int {
		// valid values are 0,1,2,3. All other values should result in 3.
		if (a > 3 || a < 0) || (b > 3 || b < 0) {
			return 3
		}

		if a > b {
			return a
		}
		return b
	}

	combinedResponse := NewCombinedResponse(list)
	for _, name := range names {
		currentCheck, ok := checkMap[name]
		if !ok {
			return nil, errors.Errorf("check %s not found", name)
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

		// list option disables the check execution
		if !list {

			start := time.Now()
			combinedOutput, code, err = currentCheck.Run(ctx, r.role)
			checkDuration = time.Since(start).String()
		}

		if _, ok := combinedResponse.checks[name]; ok {
			return nil, fmt.Errorf("duplicate check %s", name)
		}

		r := &Response{
			name:        name,
			output:      string(combinedOutput),
			code:        code,
			duration:    checkDuration,
			description: currentCheck.Description,
			fullCmd:     currentCheck.Cmd,
			timeout:     currentCheck.Timeout,
			list:        list,
		}
		combinedResponse.checks[name] = r

		// collect errors
		if err != nil {
			combinedResponse.errs[err.Error()] = r
		}

		combinedResponse.status = max(combinedResponse.status, code)
	}

	return combinedResponse, nil
}
