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

// Sign-in run on the server: `rclone authorize <type> [<id> <secret>]`,
// handing back only the sign-in link (as a path for the gateway's
// /v1/rclone-auth relay) and its outcome.
//
//   - Reconnect (POST /accounts/:name/authorize) uses the account's saved app
//     (or rclone's when it has none); the saved secret never goes to the
//     client. The token comes back - the same token the user would otherwise
//     paste - and is saved through PostCloudAccountReconnect alone, so the
//     saved app stays as it is.
//   - A new account (POST /accounts/authorize, the phone app) sends its keys
//     once; when the token arrives the server saves the account itself, so
//     neither the keys nor the token are ever sent back.
//
// One run at a time: `rclone authorize` always listens on 127.0.0.1:53682.

const authorizeTimeout = 10 * time.Minute

var (
	// rcloneBin and accountValue are swapped in tests.
	rcloneBin    = "rclone"
	accountValue = func(name, key string) string { return service.MyService.Storage().GetAttributeValueByName(name, key) }
	saveAccount  = createAccount

	// What rclone takes as a client ID/secret argument (no leading "-": it
	// would be read as a flag).
	safeKey = regexp.MustCompile(`^[A-Za-z0-9._~][A-Za-z0-9._~-]*$`)

	authLink = regexp.MustCompile(`http://(?:127\.0\.0\.1|localhost):53682(/auth\S*)`)

	authMu  sync.Mutex
	authJob *authorizeJob
)

type authorizeJob struct {
	name   string
	cancel context.CancelFunc
	// set for a new account: saved with the token when it arrives
	create *createAccountRequest
	mu     sync.Mutex
	view   authorizeView
}

type authorizeView struct {
	Running bool   `json:"running"`
	Path    string `json:"path,omitempty"`  // sign-in link, relative to /v1/rclone-auth
	Token   string `json:"token,omitempty"` // what `rclone authorize` prints to paste (reconnect only)
	Error   string `json:"error,omitempty"`
	Name    string `json:"name,omitempty"`  // a new account's remote; poll GET /accounts/<name>/authorize
	Saved   bool   `json:"saved,omitempty"` // the new account is saved and mounted
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
// app (rclone's when it has none) and returns once the link is known (or
// the run failed).
func PostCloudAccountAuthorize(c *gin.Context) {
	name := c.Param("name")
	typ, id, secret := accountValue(name, "type"), accountValue(name, "client_id"), accountValue(name, "client_secret")
	if typ == "" {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: "no such account", Data: "no such account"})
		return
	}
	if (id == "") != (secret == "") {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: "this account has no saved app of its own - enter its client ID and secret", Data: "no saved app"})
		return
	}
	startAuthorize(c, &authorizeJob{name: name}, typ, id, secret)
}

type newAuthorizeRequest struct {
	Type         string `json:"type" binding:"required"`
	Label        string `json:"label"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// PostCloudNewAccountAuthorize signs in a new OAuth account (rclone's app,
// or the user's own with client_id/client_secret) and saves it on success.
// Poll GET /accounts/<data.name>/authorize until data.saved or data.error.
func PostCloudNewAccountAuthorize(c *gin.Context) {
	var req newAuthorizeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: err.Error(), Data: err.Error()})
		return
	}
	bad := func(msg string) {
		c.JSON(common_err.CLIENT_ERROR, model.Result{Success: common_err.CLIENT_ERROR, Message: msg, Data: msg})
	}
	if !isTokenProvider(req.Type) {
		bad("this provider doesn't sign in with a browser")
		return
	}
	if (req.ClientID == "") != (req.ClientSecret == "") {
		bad("enter both the client ID and the client secret, or neither for rclone's app")
		return
	}
	if req.ClientID != "" && (!safeKey.MatchString(req.ClientID) || !safeKey.MatchString(req.ClientSecret)) {
		bad("that doesn't look like a client ID and secret")
		return
	}
	label := strings.TrimSpace(req.Label)
	if label == "" {
		label = providerLabel(req.Type)
	}
	params := map[string]string{}
	if req.ClientID != "" {
		params["client_id"], params["client_secret"] = req.ClientID, req.ClientSecret
	}
	name := remoteName(label, req.Type)
	job := &authorizeJob{name: name, create: &createAccountRequest{Type: req.Type, Label: label, Params: params}}
	job.view.Name = name
	startAuthorize(c, job, req.Type, req.ClientID, req.ClientSecret)
}

func startAuthorize(c *gin.Context, job *authorizeJob, typ, id, secret string) {
	ctx, cancel := context.WithTimeout(context.Background(), authorizeTimeout)
	job.cancel = cancel
	job.view.Running = true
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

	args := []string{"authorize", typ}
	if id != "" {
		args = append(args, id, secret)
	}
	cmd := exec.CommandContext(ctx, rcloneBin, append(args, "--auth-no-open-browser")...)
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
				if secret != "" {
					line = strings.ReplaceAll(line, secret, "***")
				}
				last.WriteString(line)
			}
		}
	}
	err := sc.Err()
	var saveErr error
	if token.Len() > 0 && job.create != nil {
		job.create.Params["token"] = token.String()
		_, saveErr = saveAccount(*job.create, job.name)
	}
	job.set(func(v *authorizeView) {
		v.Running = false
		switch {
		case saveErr != nil:
			v.Error = "signed in, but the account could not be saved: " + saveErr.Error()
		case token.Len() > 0 && job.create != nil:
			v.Saved = true
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
