//go:build compat

package compat

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// containerSpec describes one application container. Files are copied in before
// the first start so credentials exist before the application initializes.
type containerSpec struct {
	name  string
	image string
	port  int
	env   map[string]string
	files map[string]string
	args  []string
}

type container struct {
	id   string
	port int
}

// containerTool is the Makefile's CONTAINER_TOOL: docker in CI, podman works locally.
func containerTool() string {
	if tool := os.Getenv("CONTAINER_TOOL"); tool != "" {
		return tool
	}
	return "docker"
}

func runTool(args ...string) (string, error) {
	cmd := exec.Command(containerTool(), args...)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %w: %s",
			containerTool(), strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// startContainer publishes the application port on a random host port and
// removes the container when the test ends. MEDIA_OPERATOR_COMPAT_KEEP=1 keeps
// it for inspection; logs are printed whenever the test failed.
func startContainer(t *testing.T, spec containerSpec) *container {
	t.Helper()
	var id string
	var err error
	// The runtime picks a free host port when it creates the container, and with
	// many tests binding ports at once it can be taken by the time it starts.
	for range 3 {
		if id, err = createAndStart(t, spec); err == nil || !strings.Contains(err.Error(), "address already in use") {
			break
		}
	}
	require.NoError(t, err, "starting %s", spec.name)

	mapping, err := runTool("port", id, strconv.Itoa(spec.port))
	require.NoError(t, err, "reading %s port", spec.name)
	// "0.0.0.0:49153", possibly followed by an IPv6 line.
	_, hostPort, err := net.SplitHostPort(strings.Fields(mapping)[0])
	require.NoError(t, err, "parsing %s port mapping %q", spec.name, mapping)
	port, err := strconv.Atoi(hostPort)
	require.NoError(t, err, "parsing %s port %q", spec.name, hostPort)
	return &container{id: id, port: port}
}

// exec runs a shell command inside the container.
func (c *container) exec(t *testing.T, script string) string {
	t.Helper()
	out, err := runTool("exec", c.id, "sh", "-c", script)
	require.NoError(t, err, "exec %q", script)
	return out
}

// url is the application address as both the operator and other containers
// reach it: the host's outbound IP and the published port.
func (c *container) url() string {
	return fmt.Sprintf("http://%s:%d", hostIP, c.port)
}

// createAndStart creates the container, seeds its files and starts it. The
// container is removed when the test ends whether or not it started.
func createAndStart(t *testing.T, spec containerSpec) (string, error) {
	t.Helper()
	args := []string{"create", "--name", fmt.Sprintf("media-operator-compat-%s-%d", spec.name, time.Now().UnixNano()),
		"--label", "media-operator-compat=true", "-p", strconv.Itoa(spec.port)}
	for k, v := range spec.env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(append(args, spec.image), spec.args...)
	id, err := runTool(args...)
	require.NoError(t, err, "creating %s", spec.name)
	t.Cleanup(func() {
		if t.Failed() {
			if logs, err := runTool("logs", "--tail", "80", id); err == nil {
				t.Logf("%s logs:\n%s", spec.name, logs)
			}
		}
		if os.Getenv("MEDIA_OPERATOR_COMPAT_KEEP") == "1" {
			t.Logf("keeping %s container %s", spec.name, id)
			return
		}
		_, _ = runTool("rm", "-f", "-v", id)
	})

	if len(spec.files) > 0 {
		root := t.TempDir()
		for path, content := range spec.files {
			local := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(path, "/")))
			require.NoError(t, os.MkdirAll(filepath.Dir(local), 0o755))
			require.NoError(t, os.WriteFile(local, []byte(content), 0o644))
		}
		_, err := runTool("cp", root+"/.", id+":/")
		require.NoError(t, err, "seeding %s", spec.name)
	}
	_, err = runTool("start", id)
	return id, err
}
