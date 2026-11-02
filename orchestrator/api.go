package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
)

// api is the JSON client shared by the two HTTP APIs the orchestrator calls: Gitea, and Docker over its Unix socket.
type api struct {
	name, base string // name prefixes the errors
	http       *http.Client
	header     http.Header // sent with every request
}

// statusError is an answer with a status of 300 or more.
type statusError struct {
	code int
	msg  string
}

func (e *statusError) Error() string { return e.msg }

// hasStatus reports whether err is an answer with that status code.
func hasStatus(err error, code int) bool {
	var s *statusError
	return errors.As(err, &s) && s.code == code
}

// do sends in as JSON, or as a tar when it is a reader (a Docker build context), and returns the answer's body unread.
func (a *api) do(method, path string, in any) (*http.Response, error) {
	var body io.Reader
	contentType := "application/json"
	if reader, ok := in.(io.Reader); ok {
		body, contentType = reader, "application/x-tar"
	} else if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		return nil, err
	}
	maps.Copy(req.Header, a.header)
	req.Header.Set("Content-Type", contentType)
	resp, err := a.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, &statusError{resp.StatusCode, fmt.Sprintf("%s %s %s: %s %s", a.name, method, path, resp.Status, msg)}
	}
	return resp, nil
}

// call is do, with the JSON answer decoded into out unless it is nil.
func (a *api) call(method, path string, in, out any) error {
	resp, err := a.do(method, path, in)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
