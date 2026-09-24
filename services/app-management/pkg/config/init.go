package config

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/F-e-n-y-x/NivaroOS/services/app-management/common"
	"github.com/F-e-n-y-x/NivaroOS/services/app-management/model"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/constants"
	"gopkg.in/ini.v1"
)

var (
	CommonInfo = &model.CommonModel{
		RuntimePath: constants.DefaultRuntimePath,
	}

	AppInfo = &model.APPModel{
		AppStorePath: filepath.Join(constants.DefaultDataPath, "appstore"),
		AppsPath:     filepath.Join(constants.DefaultDataPath, "apps"),
		LogPath:      constants.DefaultLogPath,
		LogSaveName:  common.AppManagementServiceName,
		LogFileExt:   "log",
	}

	ServerInfo = &model.ServerModel{
		AppStoreList: []string{},
	}

	// Global is a map to inject environment variables to the app.
	Global = make(map[string]string)

	NivaroOSGlobalVariables = &model.NivaroOSGlobalVariables{}

	Cfg               *ini.File
	ConfigFilePath    string
	GlobalEnvFilePath string

	// cfgMu guards Cfg and ServerInfo.AppStoreList, which are read and
	// replaced from HTTP handlers, the register goroutine and the catalog cron.
	cfgMu sync.RWMutex

	// globalMu guards Global; saveGlobalMu serialises writes of the env file.
	globalMu     sync.RWMutex
	saveGlobalMu sync.Mutex
)

func ReloadConfig() {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	cfg, err := ini.LoadSources(ini.LoadOptions{Insensitive: true, AllowShadows: true}, ConfigFilePath)
	if err != nil {
		fmt.Println("failed to reload config", err)
	} else {
		Cfg = cfg
		mapTo("common", CommonInfo)
		mapTo("app", AppInfo)
		mapTo("server", ServerInfo)
	}
}

// AppStoreList returns a copy of the registered app store URLs.
func AppStoreList() []string {
	cfgMu.RLock()
	defer cfgMu.RUnlock()

	return append([]string{}, ServerInfo.AppStoreList...)
}

// AddAppStore appends url to the app store list and saves the config file.
// It returns false (and changes nothing) when the url is already registered
// (case-insensitive).
func AddAppStore(url string) (bool, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	for _, u := range ServerInfo.AppStoreList {
		if strings.EqualFold(u, url) {
			return false, nil
		}
	}

	ServerInfo.AppStoreList = append(append([]string{}, ServerInfo.AppStoreList...), url)

	return true, saveSetupLocked()
}

// RemoveAppStore removes url (case-insensitive) from the app store list and
// saves the config file. It returns false when the url is not registered.
func RemoveAppStore(url string) (bool, error) {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	list := make([]string, 0, len(ServerInfo.AppStoreList))
	found := false
	for _, u := range ServerInfo.AppStoreList {
		if !found && strings.EqualFold(u, url) {
			found = true
			continue
		}
		list = append(list, u)
	}

	if !found {
		return false, nil
	}

	ServerInfo.AppStoreList = list

	return true, saveSetupLocked()
}

func InitSetup(config string, sample string) {
	ConfigFilePath = AppManagementConfigFilePath
	if len(config) > 0 {
		ConfigFilePath = config
	}

	// create default config file if not exist
	if _, err := os.Stat(ConfigFilePath); os.IsNotExist(err) {
		fmt.Println("config file not exist, create it")
		// create config file
		file, err := os.Create(ConfigFilePath)
		if err != nil {
			panic(err)
		}
		defer file.Close()

		// write default config
		_, err = file.WriteString(sample)
		if err != nil {
			panic(err)
		}
	}

	var err error

	Cfg, err = ini.LoadSources(ini.LoadOptions{Insensitive: true, AllowShadows: true}, ConfigFilePath)
	if err != nil {
		panic(err)
	}

	mapTo("common", CommonInfo)
	mapTo("app", AppInfo)
	mapTo("server", ServerInfo)
}

func SaveSetup() error {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	return saveSetupLocked()
}

func saveSetupLocked() error {
	reflectFrom("common", CommonInfo)
	reflectFrom("app", AppInfo)
	reflectFrom("server", ServerInfo)

	return Cfg.SaveTo(ConfigFilePath)
}

// GlobalEnvFilePathFor returns the global env file that belongs to the given
// -c config file: the `env` file next to it. Without -c the default is used.
//
// (InitGlobal used to be handed the -c path itself and parsed the ini config
// file as KEY=VALUE lines, so settings like "LogPath" were injected into every
// container while the real env file was never read.)
func GlobalEnvFilePathFor(configFile string) string {
	if len(configFile) == 0 {
		return AppManagementGlobalEnvFilePath
	}

	return filepath.Join(filepath.Dir(configFile), filepath.Base(AppManagementGlobalEnvFilePath))
}

func InitGlobal(configFile string) {
	// from file read key and value
	// file content like this:
	// OPENAI_API_KEY=123456
	GlobalEnvFilePath = GlobalEnvFilePathFor(configFile)

	file, err := os.Open(GlobalEnvFilePath)
	if err != nil {
		log.Println("open global env file error:", err)
		return
	}
	defer file.Close()

	globalMu.Lock()
	defer globalMu.Unlock()

	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			Global[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
}

var globalKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidateGlobal checks a global env key/value before it is stored: the file
// is written as KEY=VALUE lines, so a newline in either would inject lines.
func ValidateGlobal(key, value string) error {
	if !globalKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q - use letters, digits and underscores only", key)
	}

	if strings.ContainsAny(value, "\r\n\x00") {
		return fmt.Errorf("the value of %s must not contain line breaks", key)
	}

	return nil
}

// GlobalSnapshot returns a copy of the global env map.
func GlobalSnapshot() map[string]string {
	globalMu.RLock()
	defer globalMu.RUnlock()

	result := make(map[string]string, len(Global))
	for k, v := range Global {
		result[k] = v
	}

	return result
}

func GetGlobal(key string) (string, bool) {
	globalMu.RLock()
	defer globalMu.RUnlock()

	value, ok := Global[key]
	return value, ok
}

func SetGlobal(key, value string) error {
	if err := ValidateGlobal(key, value); err != nil {
		return err
	}

	globalMu.Lock()
	defer globalMu.Unlock()

	Global[key] = value
	return nil
}

func DeleteGlobal(key string) {
	globalMu.Lock()
	defer globalMu.Unlock()

	delete(Global, key)
}

func SaveGlobal() error {
	// file content like this:
	// OPENAI_API_KEY=123456
	saveGlobalMu.Lock()
	defer saveGlobalMu.Unlock()

	path := GlobalEnvFilePath
	if path == "" {
		path = AppManagementGlobalEnvFilePath
	}

	global := GlobalSnapshot()
	keys := make([]string, 0, len(global))
	for k := range global {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&buf, "%s=%s\n", key, global[key])
	}

	// write to a temp file and rename, so a crash never leaves a half-written file
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(buf.String()), 0o600); err != nil {
		return err
	}

	return os.Rename(tmp, path)
}

func mapTo(section string, v interface{}) {
	err := Cfg.Section(section).MapTo(v)
	if err != nil {
		log.Fatalf("Cfg.MapTo %s err: %v", section, err)
	}
}

func reflectFrom(section string, v interface{}) {
	err := Cfg.Section(section).ReflectFrom(v)
	if err != nil {
		log.Fatalf("Cfg.ReflectFrom %s err: %v", section, err)
	}
}
