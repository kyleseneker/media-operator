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
	args := []string{"create", "--name", fmt.Sprintf("media-operator-compat-%s-%d", spec.name, time.Now().UnixNano()),
		"--label", "media-operator-compat=true", "-p", strconv.Itoa(spec.port)}
	for k, v := range spec.env {
		args = append(args, "-e", k+"="+v)
	}
	args = append(append(args, spec.image), spec.args...)
	id, err := runTool(args...)
	if err != nil {
		t.Fatalf("creating %s: %v", spec.name, err)
	}
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
			if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(local, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := runTool("cp", root+"/.", id+":/"); err != nil {
			t.Fatalf("seeding %s: %v", spec.name, err)
		}
	}
	if _, err := runTool("start", id); err != nil {
		t.Fatalf("starting %s: %v", spec.name, err)
	}

	mapping, err := runTool("port", id, strconv.Itoa(spec.port))
	if err != nil {
		t.Fatalf("reading %s port: %v", spec.name, err)
	}
	// "0.0.0.0:49153", possibly followed by an IPv6 line.
	_, hostPort, err := net.SplitHostPort(strings.Fields(mapping)[0])
	if err != nil {
		t.Fatalf("parsing %s port mapping %q: %v", spec.name, mapping, err)
	}
	port, err := strconv.Atoi(hostPort)
	if err != nil {
		t.Fatalf("parsing %s port %q: %v", spec.name, hostPort, err)
	}
	return &container{id: id, port: port}
}

// exec runs a shell command inside the container.
func (c *container) exec(t *testing.T, script string) string {
	t.Helper()
	out, err := runTool("exec", c.id, "sh", "-c", script)
	if err != nil {
		t.Fatalf("exec %q: %v", script, err)
	}
	return out
}

// url is the application address as both the operator and other containers
// reach it: the host's outbound IP and the published port.
func (c *container) url() string {
	return fmt.Sprintf("http://%s:%d", hostIP, c.port)
}
