package main

import (
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
type dockerEngine struct{ api }

func newDocker(socket string) *dockerEngine {
	return &dockerEngine{api{name: "docker", base: "http://docker", http: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socket)
		},
	}}}}
}

// run creates and starts a container; the id is returned even when start fails, so the caller can remove it.
func (d *dockerEngine) run(image, network string, env []string) (string, error) {
	var created struct {
		ID string `json:"Id"`
	}
	spec := map[string]any{"Image": image, "Env": env, "Labels": map[string]string{agentLabel: "1"}, "HostConfig": map[string]any{"NetworkMode": network}}
	if err := d.call("POST", "/containers/create", spec, &created); err != nil {
		return "", err
	}
	return created.ID, d.call("POST", "/containers/"+created.ID+"/start", nil, nil)
}

// agentLabel marks the one-shot agent containers, so a restarted orchestrator can remove the ones its previous run left.
const agentLabel = "forge.agent"

// removeAgents force-removes every agent container, running or not, and returns how many it removed.
func (d *dockerEngine) removeAgents() (int, error) {
	var containers []struct {
		ID string `json:"Id"`
	}
	filter := url.QueryEscape(`{"label":["` + agentLabel + `"]}`)
	if err := d.call("GET", "/containers/json?all=1&filters="+filter, nil, &containers); err != nil {
		return 0, err
	}
	for i, c := range containers {
		if err := d.remove(c.ID); err != nil {
			return i, err
		}
	}
	return len(containers), nil
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

// serve replaces the container called name by one running image, its port published on hostIP:hostPort.
func (d *dockerEngine) serve(name, image, network, port, hostIP, hostPort string) error {
	if err := d.remove(name); err != nil && !hasStatus(err, http.StatusNotFound) {
		return err
	}
	spec := map[string]any{
		"Image":        image,
		"ExposedPorts": map[string]any{port + "/tcp": struct{}{}},
		"HostConfig": map[string]any{
			"NetworkMode":   network,
			"RestartPolicy": map[string]string{"Name": "unless-stopped"},
			"PortBindings":  map[string]any{port + "/tcp": []map[string]string{{"HostIp": hostIP, "HostPort": hostPort}}},
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

// stderrTail returns the last lines the container wrote to stderr, or "" when they cannot be read.
func (d *dockerEngine) stderrTail(id string, lines int) string {
	logs, err := d.logs(id, fmt.Sprintf("stderr=1&tail=%d", lines))
	if err != nil {
		return ""
	}
	defer logs.Close()
	out, _ := io.ReadAll(logs)
	return strings.TrimSpace(string(out))
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
