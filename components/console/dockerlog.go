package log

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/TechAlkurn/core/action"
	"github.com/TechAlkurn/core/lib"
)

const (
	dockerLogDefaultTail = 200
	dockerLogMaxTail     = 5000
	dockerLogMaxBytes    = 8 << 20 // cap the payload at 8MB
)

// DockerLog returns the tail of `docker logs amlakserv_app` (snapshot — a
// unary RPC cannot follow like `-f`; poll with ?since= instead).
//
// It talks to the Docker Engine API directly, so no docker CLI is needed.
// DOCKER_HOST picks the endpoint (same convention as the docker CLI):
//   - tcp://docker-proxy:2375  in compose (read-only docker-socket-proxy)
//   - unix:///var/run/docker.sock  default, for `go run main.go` on the host
//
// Query params: tail (default 200, max 5000), since (unix ts or Go duration
// like "10m"), timestamps (1/true).
// Env: DOCKER_HOST, DOCKER_CONTAINER (default amlakserv_app).
//
// rawQuery is the request's raw query string (gen.Request.GetQuery() on the
// host), parsed the same way register.GrpcAction.Query does. Core never
// imports host protos, so the caller passes the string, not the message.
func DockerLog(c context.Context, rawQuery string) (map[string]any, error) {
	srv := action.Request(rawQuery)
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}
	container := os.Getenv("DOCKER_CONTAINER")
	if container == "" {
		return nil, fmt.Errorf("docker container not found")
	}
	tail := dockerLogDefaultTail
	if v := lib.ToInt(srv.Query("tail")); v > 0 {
		tail = min(v, dockerLogMaxTail)
	}

	q := url.Values{}
	q.Set("stdout", "1")
	q.Set("stderr", "1")
	q.Set("tail", fmt.Sprint(tail))
	if since := strings.TrimSpace(srv.Query("since")); since != "" {
		if d, err := time.ParseDuration(since); err == nil {
			q.Set("since", fmt.Sprint(time.Now().Add(-d).Unix()))
		} else if ts := lib.ToInt(since); ts > 0 {
			q.Set("since", fmt.Sprint(ts))
		}
	}
	if ts := srv.Query("timestamps"); ts == "1" || ts == "true" {
		q.Set("timestamps", "1")
	}

	client := &http.Client{Timeout: 15 * time.Second}
	base := "http://docker"
	switch {
	case strings.HasPrefix(dockerHost, "unix://"):
		sock := strings.TrimPrefix(dockerHost, "unix://")
		client.Transport = &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sock)
			},
		}
	case strings.HasPrefix(dockerHost, "tcp://"):
		base = "http://" + strings.TrimPrefix(dockerHost, "tcp://")
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST %q (use unix:// or tcp://)", dockerHost)
	}
	endpoint := base + "/containers/" + url.PathEscape(container) + "/logs?" + q.Encode()
	req, err := http.NewRequestWithContext(c, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker API %s unreachable (is docker-proxy running?): %w", dockerHost, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, dockerLogMaxBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker logs: status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	lines := strings.Split(strings.TrimRight(demuxDockerLog(raw), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = []string{}
	}
	return map[string]any{
		"container": container,
		"tail":      tail,
		"count":     len(lines),
		"lines":     lines,
	}, nil
}

// demuxDockerLog strips the 8-byte stream headers Docker prepends to each
// frame for non-TTY containers ([stream,0,0,0,size(BE uint32)]). Output from
// a TTY container has no headers and is returned unchanged.
func demuxDockerLog(raw []byte) string {
	if len(raw) < 8 || raw[0] > 2 || raw[1] != 0 || raw[2] != 0 || raw[3] != 0 {
		return string(raw)
	}
	var out strings.Builder
	for len(raw) >= 8 {
		size := int(raw[4])<<24 | int(raw[5])<<16 | int(raw[6])<<8 | int(raw[7])
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw) // truncated by the byte cap
		}
		out.Write(raw[:size])
		raw = raw[size:]
	}
	return out.String()
}
