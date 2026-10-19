package v1

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/common_err"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/service"
	"github.com/gin-gonic/gin"
)

// Reconnect with the account's own OAuth app (Settings > Online Storage >
// Reconnect). The saved client secret never goes to the browser: this runs
// `rclone authorize <type> <saved id> <saved secret>` here and hands back
// only the sign-in link (as a path for the gateway's /v1/rclone-auth relay)
// and, once the user has signed in, the token - the same token the user
// would otherwise paste. Saving still goes through PostCloudAccountReconnect
// with just the token, so the saved app stays as it is.
//
// One run at a time: `rclone authorize` always listens on 127.0.0.1:53682.

const authorizeTimeout = 10 * time.Minute

var (
	// rcloneBin and accountValue are swapped in tests.
	rcloneBin    = "rclone"
	accountValue = func(name, key string) string { return service.MyService.Storage().GetAttributeValueByName(name, key) }

	authLink = regexp.MustCompile(`http://(?:127\.0\.0\.1|localhost):53682(/auth\S*)`)

	authMu  sync.Mutex
	authJob *authorizeJob
)

type authorizeJob struct {
	name   string
	cancel context.CancelFunc
	mu     sync.Mutex
	view   authorizeView
}

type authorizeView struct {
	Running bool   `json:"running"`
	Path    string `json:"path,omitempty"`  // sign-in link, relative to /v1/rclone-auth
	Token   string `json:"token,omitempty"` // what `rclone authorize` prints to paste
	Error   string `json:"error,omitempty"`
}

func (j *authorizeJob) get() authorizeView {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.view
}

func (j *authorizeJob) set(f func(v *authorizeView)) {
	j.mu.Lock()
	f(&j.view)
	j.mu.Unlock()
}

// PostCloudAccountAuthorize starts the sign-in with the account's saved
// app and returns once the link is known (or the run failed).
func PostCloudAccountAuthorize(c *gin.Context) {
	name := c.Param("name")
	typ, id, secret := accountValue(name, "type"), accountValue(name, "client_id"), accountValue(name, "client_secret")
	if typ == "" || id == "" || secret == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: "this account has no saved app of its own - enter its client ID and secret", Data: "no saved app"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), authorizeTimeout)
	job := &authorizeJob{name: name, cancel: cancel, view: authorizeView{Running: true}}
	authMu.Lock()
	if authJob != nil {
		authJob.cancel() // frees port 53682 for this run
	}
	authJob = job
	authMu.Unlock()

	linked := make(chan struct{})
	go runAuthorize(ctx, job, linked, typ, id, secret)
	select {
	case <-linked:
	case <-time.After(30 * time.Second):
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: job.get()})
}

// GetCloudAccountAuthorize polls the run; data is null when none is
// running for this account.
func GetCloudAccountAuthorize(c *gin.Context) {
	authMu.Lock()
	job := authJob
	authMu.Unlock()
	var data any
	if job != nil && job.name == c.Param("name") {
		data = job.get()
	}
	c.JSON(common_err.SUCCESS, model.Result{Success: common_err.SUCCESS, Message: common_err.GetMsg(common_err.SUCCESS), Data: data})
}

// runAuthorize closes linked once the link (or the end of the run) is known.
// ponytail: the secret is in rclone's argv for the run (as with the terminal
// command before); pass it another way if local users must not see it.
func runAuthorize(ctx context.Context, job *authorizeJob, linked chan struct{}, typ, id, secret string) {
	defer job.cancel()
	var once sync.Once
	link := func() { once.Do(func() { close(linked) }) }
	defer link()

	cmd := exec.CommandContext(ctx, rcloneBin, "authorize", typ, id, secret, "--auth-no-open-browser")
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		job.set(func(v *authorizeView) { v.Running, v.Error = false, "could not start rclone: "+err.Error() })
		return
	}
	go func() { pw.CloseWithError(cmd.Wait()) }()

	var token, last strings.Builder
	inToken := false
	sc := bufio.NewScanner(pr)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasSuffix(line, "--->"):
			inToken = true
		case strings.HasPrefix(line, "<---End paste"):
			inToken = false
		case inToken:
			token.WriteString(line)
		default:
			if m := authLink.FindStringSubmatch(line); m != nil {
				job.set(func(v *authorizeView) { v.Path = m[1] })
				link()
			} else if line != "" {
				last.Reset()
				last.WriteString(strings.ReplaceAll(line, secret, "***"))
			}
		}
	}
	err := sc.Err()
	job.set(func(v *authorizeView) {
		v.Running = false
		switch {
		case token.Len() > 0:
			v.Token = token.String()
		case ctx.Err() == context.DeadlineExceeded:
			v.Error = "the sign-in took too long - start it again"
		case ctx.Err() != nil:
			v.Error = "stopped"
		case last.Len() > 0:
			v.Error = last.String()
		case err != nil:
			v.Error = err.Error()
		default:
			v.Error = "rclone ended without a token"
		}
	})
}
