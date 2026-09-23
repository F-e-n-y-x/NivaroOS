package service

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ContainerSource says where a container's image comes from, which decides
// how it can be updated:
//   - registry: a pullable image (Docker Hub, ghcr.io, ...) - pull + recreate
//   - git: built by compose from a folder that is a git checkout - fetch to
//     see if upstream (e.g. GitHub) has new commits; update = pull + rebuild
//   - local: built by compose from a plain folder - no upstream to compare,
//     "Rebuild" rebuilds it (refreshing base images)
//
// Updates used to assume every image lives in a registry: for images built
// from local compose projects the pull failed and nothing could update them.
type ContainerSource struct {
	Kind        string `json:"kind"`
	Project     string `json:"project,omitempty"`
	Service     string `json:"service,omitempty"`
	ComposeFile string `json:"compose_file,omitempty"`
	EnvFile     string `json:"-"`
	Context     string `json:"context,omitempty"`
	Repo        string `json:"repo,omitempty"`
	Remote      string `json:"remote,omitempty"` // without credentials
	Branch      string `json:"branch,omitempty"` // "" when detached
	Behind      int    `json:"behind"`
}

// hostPathMapper maps a path as seen inside another container (Portainer
// keeps stacks under its own /data) to the host path, or returns it as is.
type hostPathMapper func(string) string

func detectSource(labels map[string]string, toHost hostPathMapper) ContainerSource {
	src := ContainerSource{Kind: "registry", Project: labels["com.docker.compose.project"], Service: labels["com.docker.compose.service"]}
	files := labels["com.docker.compose.project.config_files"]
	if src.Project == "" || src.Service == "" || files == "" {
		return src
	}
	file := toHost(strings.Split(files, ",")[0])
	raw, err := os.ReadFile(file)
	if err != nil {
		return src
	}
	var doc struct {
		Services map[string]struct {
			Build yaml.Node `yaml:"build"`
		} `yaml:"services"`
	}
	if yaml.Unmarshal(raw, &doc) != nil {
		return src
	}
	svc, ok := doc.Services[src.Service]
	if !ok || svc.Build.Kind == 0 {
		return src // no build: the image comes from a registry
	}
	buildContext := ""
	switch svc.Build.Kind {
	case yaml.ScalarNode:
		buildContext = svc.Build.Value
	case yaml.MappingNode:
		var b struct {
			Context string `yaml:"context"`
		}
		_ = svc.Build.Decode(&b)
		buildContext = b.Context
	}
	if buildContext == "" {
		buildContext = "."
	}
	if !filepath.IsAbs(buildContext) {
		buildContext = filepath.Join(filepath.Dir(file), buildContext)
	}
	src.ComposeFile = file
	src.Context = filepath.Clean(buildContext)
	if env := filepath.Join(filepath.Dir(file), "stack.env"); fileExists(env) {
		src.EnvFile = env
	} else if env := filepath.Join(filepath.Dir(file), ".env"); fileExists(env) {
		src.EnvFile = env
	}
	src.Kind = "local"
	if top := gitOut(src.Context, "rev-parse", "--show-toplevel"); top != "" {
		if remote := gitOut(top, "remote", "get-url", "origin"); remote != "" {
			src.Kind = "git"
			src.Repo = top
			src.Remote = stripCredentials(remote)
			if b := gitOut(top, "rev-parse", "--abbrev-ref", "HEAD"); b != "HEAD" {
				src.Branch = b
			}
		}
	}
	return src
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func gitCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "safe.directory=*"}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	return cmd
}

func gitOut(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := gitCmd(ctx, dir, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func stripCredentials(remote string) string {
	if u, err := url.Parse(remote); err == nil && u.User != nil {
		u.User = nil
		return u.String()
	}
	return remote
}

// gitBehind fetches the remote and counts commits the checkout is behind
// its upstream branch.
func gitBehind(ctx context.Context, src ContainerSource) (int, error) {
	if src.Branch == "" {
		return 0, fmt.Errorf("the checkout at %s is not on a branch (detached HEAD) - check out a branch to follow updates", src.Repo)
	}
	if out, err := gitCmd(ctx, src.Repo, "fetch", "--quiet", "origin", src.Branch).CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "could not read Username") || strings.Contains(msg, "Authentication failed") {
			return 0, fmt.Errorf("%s is private - the server needs a GitHub login to check it (store a personal access token: git -C %s config credential.helper store, then run one git fetch there)", src.Remote, src.Repo)
		}
		return 0, fmt.Errorf("git fetch: %s", msg)
	}
	out, err := gitCmd(ctx, src.Repo, "rev-list", "--count", "HEAD..origin/"+src.Branch).Output()
	if err != nil {
		return 0, fmt.Errorf("comparing with origin/%s: %w", src.Branch, err)
	}
	return strconv.Atoi(strings.TrimSpace(string(out)))
}

// composeArgs is the `docker compose` prefix for a source's project.
func composeArgs(src ContainerSource) []string {
	args := []string{"compose", "-p", src.Project, "-f", src.ComposeFile}
	if src.EnvFile != "" {
		args = append(args, "--env-file", src.EnvFile)
	}
	return args
}

// rebuildSteps is what updating a compose-built container runs: pull the
// new commits (git only; --ff-only never overwrites local changes), build
// with fresh base images, then recreate just that service - started only
// if it was running (a stopped container stays stopped).
func rebuildSteps(src ContainerSource, wasRunning bool) [][]string {
	var steps [][]string
	if src.Kind == "git" {
		steps = append(steps, []string{"git", "-C", src.Repo, "-c", "safe.directory=*", "pull", "--ff-only", "origin", src.Branch})
	}
	base := composeArgs(src)
	steps = append(steps, append(append([]string{"docker"}, base...), "build", "--pull", src.Service))
	if wasRunning {
		steps = append(steps, append(append([]string{"docker"}, base...), "up", "-d", "--no-deps", src.Service))
	} else {
		steps = append(steps, append(append([]string{"docker"}, base...), "up", "--no-start", "--no-deps", src.Service))
	}
	return steps
}
