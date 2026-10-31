package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// dockerEngine talks to the Docker Engine API over its Unix socket: the few calls needed don't justify the SDK.
type dockerEngine struct {
	http *http.Client
}

func newDocker(socket string) *dockerEngine {
	return &dockerEngine{http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}
}

func (d *dockerEngine) do(method, path string, in any) (*http.Response, error) {
	var body io.Reader
	contentType := "application/json"
	if context, ok := in.(io.Reader); ok {
		body, contentType = context, "application/x-tar" // a build context
	} else if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, "http://docker"+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return nil, fmt.Errorf("docker %s %s: %s %s", method, path, resp.Status, msg)
	}
	return resp, nil
}

func (d *dockerEngine) call(method, path string, in, out any) error {
	resp, err := d.do(method, path, in)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// run creates and starts a container; the id is returned even when start fails, so the caller can remove it.
func (d *dockerEngine) run(image, network string, env []string) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	spec := map[string]any{"Image": image, "Env": env, "HostConfig": map[string]any{"NetworkMode": network}}
	if err := d.call("POST", "/containers/create", spec, &created); err != nil {
		return "", err
	}
	return created.ID, d.call("POST", "/containers/"+created.ID+"/start", nil, nil)
}

// build builds an image from a tar build context. Build failures arrive inside the JSON stream, not as a status.
func (d *dockerEngine) build(context io.Reader, tag string) error {
	resp, err := d.do("POST", "/build?rm=1&forcerm=1&t="+url.QueryEscape(tag), context)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var tail []string // the last build output lines, to explain a failure
	for decoder := json.NewDecoder(resp.Body); ; {
		var msg struct{ Stream, Error string }
		if err := decoder.Decode(&msg); err == io.EOF {
			return nil
		} else if err != nil {
			return err
		}
		if msg.Error != "" {
			return fmt.Errorf("docker build: %s\n%s", msg.Error, strings.Join(tail, ""))
		}
		if tail = append(tail, msg.Stream); len(tail) > 10 {
			tail = tail[1:]
		}
	}
}

// serve replaces the container called name by one running image, its port published on localhost:hostPort.
func (d *dockerEngine) serve(name, image, network, port, hostPort string) error {
	if err := d.remove(name); err != nil && !strings.Contains(err.Error(), "404") {
		return err
	}
	spec := map[string]any{
		"Image":        image,
		"ExposedPorts": map[string]any{port + "/tcp": struct{}{}},
		"HostConfig": map[string]any{
			"NetworkMode":   network,
			"RestartPolicy": map[string]string{"Name": "unless-stopped"},
			"PortBindings":  map[string]any{port + "/tcp": []map[string]string{{"HostIp": "127.0.0.1", "HostPort": hostPort}}},
		},
	}
	if err := d.call("POST", "/containers/create?name="+name, spec, nil); err != nil {
		return err
	}
	return d.call("POST", "/containers/"+name+"/start", nil, nil)
}

// logs streams the container logs selected by query, stripped of Docker's stream framing.
func (d *dockerEngine) logs(id, query string) (io.ReadCloser, error) {
	resp, err := d.do("GET", "/containers/"+id+"/logs?"+query, nil)
	if err != nil {
		return nil, err
	}
	reader, writer := io.Pipe()
	go func() {
		writer.CloseWithError(demux(resp.Body, writer))
		resp.Body.Close()
	}()
	return reader, nil
}

func (d *dockerEngine) wait(id string) (int, error) {
	var result struct{ StatusCode int }
	err := d.call("POST", "/containers/"+id+"/wait", nil, &result)
	return result.StatusCode, err
}

func (d *dockerEngine) remove(id string) error {
	return d.call("DELETE", "/containers/"+id+"?force=1", nil, nil)
}

// demux copies the payloads of Docker's multiplexed log frames (8-byte header, size in bytes 4-7) to w.
func demux(r io.Reader, w io.Writer) error {
	header := make([]byte, 8)
	for {
		if _, err := io.ReadFull(r, header); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if _, err := io.CopyN(w, r, int64(binary.BigEndian.Uint32(header[4:]))); err != nil {
			return err
		}
	}
}
