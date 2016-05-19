package api

import (
	"time"
	log "github.com/Sirupsen/logrus"
	"net/http"
	"io/ioutil"
	"io"
)

// Implementation of HTTPRequester
type HTTPRequest struct {}

func (h *HTTPRequest) doRequest(method, url string, timeout time.Duration, body io.Reader) (responseBody []byte, httpResponseCode int, err error){
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return responseBody, http.StatusBadRequest, err
	}

	resp, err := h.MakeRequest(request, timeout)
	if err != nil {
		return responseBody, http.StatusBadRequest, err
	}

	defer resp.Body.Close()
	responseBody, err = ioutil.ReadAll(resp.Body)
	return responseBody, resp.StatusCode, nil
}

func (h *HTTPRequest) Get(url string, timeout time.Duration) (body []byte, httpResponseCode int, err error) {
	log.Debugf("GET %s, timeout: %s", url, timeout.String())
	return h.doRequest("GET", url, timeout, nil)

}

func (h *HTTPRequest) Post(url string, timeout time.Duration) (body []byte, httpResponseCode int, err error) {
	log.Debugf("POST %s, timeout: %s", url, timeout.String())
	return h.doRequest("POST", url, timeout, nil)
}

func (h *HTTPRequest) MakeRequest(req *http.Request, timeout time.Duration) (resp *http.Response, err error) {
	client := http.Client{
		Timeout: timeout,
	}
	resp, err = client.Do(req)
	if err != nil {
		return resp, err
	}
	// the user of this function is responsible to close the response body.
	return resp, nil
}
