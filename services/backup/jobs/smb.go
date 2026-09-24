package jobs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
)

// SMB network shares (endpoint kind smb): RefID is the id of a connection
// core stores in its database (Storage > Network shares). The engine
// reaches the share with rclone's smb backend and per-run credentials
// (spec §6.2), so the job side has to hand it host, share, user and
// password. core's API never returns passwords, and must not start to, so
// the service reads core's o_connections table directly, read-only.

// Where core keeps its database: casaos.conf's DBPath + /db/casaOS.db
// (services/core main.go and pkg/sqlite). DefaultCoreDB is that path with
// the stock DBPath; CoreDBPath reads the real one.
const (
	coreConfFile  = constants.DefaultConfigPath + "/casaos.conf"
	DefaultCoreDB = constants.DefaultDataPath + "/db/casaOS.db"
)

// CoreDBPath returns core's database path from its config file (the
// DBPath key, in any section), or DefaultCoreDB when the file or the key
// is missing - an install whose DBPath was changed still finds it.
func CoreDBPath(confFile string) string {
	raw, err := os.ReadFile(confFile)
	if err != nil {
		return DefaultCoreDB
	}
	for _, line := range strings.Split(string(raw), "\n") {
		key, val, ok := strings.Cut(line, "=")
		if !ok || strings.TrimSpace(key) != "DBPath" {
			continue
		}
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if strings.HasPrefix(val, "/") {
			return filepath.Join(filepath.Clean(val), "db", "casaOS.db")
		}
	}
	return DefaultCoreDB
}

// SMBConnection is one saved network-share connection.
type SMBConnection struct {
	ID         string
	Host       string
	Port       string
	User       string
	Password   string
	Shares     []string // browsable shares, as core mounted them
	MountPoint string   // /mnt/<host>; each share at MountPoint/<share>
}

// Label is how the UI names the server: \\host.
func (c SMBConnection) Label() string { return `\\` + c.Host }

// SMBSource lists saved connections.
type SMBSource interface {
	Connections(ctx context.Context) ([]SMBConnection, error)
}

// CoreDBSMB reads connections from core's SQLite database. A missing
// database (core never started, or a different layout) means none.
type CoreDBSMB struct {
	Path string // "" = CoreDBPath(/etc/nivaroos/casaos.conf)

	mu sync.Mutex
	db *gorm.DB
}

type coreConnectionRow struct {
	ID          uint
	Username    string
	Password    string
	Host        string
	Port        string
	Directories string
	MountPoint  string
}

func (c *CoreDBSMB) open() (*gorm.DB, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db != nil {
		return c.db, nil
	}
	path := c.Path
	if path == "" {
		path = CoreDBPath(coreConfFile)
	}
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?mode=ro&_pragma=busy_timeout(3000)"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	c.db = db
	return db, nil
}

func (c *CoreDBSMB) Connections(ctx context.Context) ([]SMBConnection, error) {
	db, err := c.open()
	if errors.Is(err, os.ErrNotExist) {
		return []SMBConnection{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("core database: %w", err)
	}
	var rows []coreConnectionRow
	if err := db.WithContext(ctx).Table("o_connections").Find(&rows).Error; err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return []SMBConnection{}, nil
		}
		return nil, fmt.Errorf("core database: %w", err)
	}
	out := make([]SMBConnection, 0, len(rows))
	for _, r := range rows {
		var shares []string
		for _, s := range strings.Split(r.Directories, ",") {
			s = strings.TrimSpace(s)
			if s != "" && !strings.HasSuffix(s, "$") {
				shares = append(shares, s)
			}
		}
		out = append(out, SMBConnection{
			ID: strconv.FormatUint(uint64(r.ID), 10), Host: r.Host, Port: r.Port, User: r.Username,
			Password: r.Password, Shares: shares, MountPoint: r.MountPoint,
		})
	}
	return out, nil
}

// smbCredsFor returns the credentials of every smb endpoint in eps, keyed
// by RefID. An endpoint whose connection was deleted is endpoint_unknown.
func (s *Service) smbCredsFor(ctx context.Context, eps []Endpoint) (map[string]engine.SMBCreds, error) {
	var need []Endpoint
	for _, ep := range eps {
		if ep.Kind == EPSMB {
			need = append(need, ep)
		}
	}
	if len(need) == 0 {
		return nil, nil
	}
	if s.smb == nil {
		return nil, engine.Errorf(engine.CodeEndpointUnknown, "no network share connections available")
	}
	conns, err := s.smb.Connections(ctx)
	if err != nil {
		return nil, engine.Errorf(engine.CodeEndpointUnknown, "network share connections: %w", err)
	}
	byID := map[string]SMBConnection{}
	for _, c := range conns {
		byID[c.ID] = c
	}
	out := map[string]engine.SMBCreds{}
	for _, ep := range need {
		c, ok := byID[ep.RefID]
		if !ok {
			return nil, engine.Errorf(engine.CodeEndpointUnknown, "network share connection %s no longer exists", ep.RefID)
		}
		share, _, _ := strings.Cut(ep.SubPath, "/")
		out[ep.RefID] = engine.SMBCreds{Host: c.Host, Share: share, User: c.User, Password: c.Password}
	}
	return out, nil
}
